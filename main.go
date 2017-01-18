package main

import (
	"bufio"
	"fmt"
	"io"
	"log"
	"net/url"
	"os"
	"sort"
	"strings"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/ec2"
)

type Instance struct {
	Name             string
	Id               string
	Type             string
	PrivateIpAddress string
	PublicIpAddress  string
}

func (i *Instance) String() string {
	return fmt.Sprintf("name: %s, id: %s, type: %s, private ip: %s, public ip: %s",
		i.Name, i.Id, i.Type, i.PrivateIpAddress, i.PublicIpAddress)
}

type Instances []*Instance

func (s Instances) Len() int {
	return len(s)
}

func (s Instances) Less(i, j int) bool {
	return s[i].Name < s[j].Name
}

func (s Instances) Swap(i, j int) {
	s[i], s[j] = s[j], s[i]
}

type State uint8

const (
	Before State = iota
	Inside
	After
)

func GetInstance(inst *ec2.Instance) (*Instance, error) {
	myInst := Instance{}
	for _, keys := range inst.Tags {
		if *keys.Key == "Name" {
			myInst.Name = url.QueryEscape(*keys.Value)
		}
	}

	if inst.InstanceId != nil {
		myInst.Id = *inst.InstanceId
	}

	if inst.InstanceType != nil {
		myInst.Type = *inst.InstanceType
	}

	if inst.PrivateIpAddress != nil {
		myInst.PrivateIpAddress = *inst.PrivateIpAddress
	}

	if inst.PublicIpAddress != nil {
		myInst.PublicIpAddress = *inst.PublicIpAddress
	}

	return &myInst, nil
}

func GetInstances(svc *ec2.EC2, filters map[string]string, tagFilters map[string]string) (Instances, error) {
	myFilters := []*ec2.Filter{
		&ec2.Filter{
			Name: aws.String("instance-state-name"),
			Values: []*string{
				aws.String("running"),
			},
		},
	}

	for key, value := range filters {
		myFilters = append(myFilters, &ec2.Filter{
			Name: aws.String(key),
			Values: []*string{
				aws.String(value),
			},
		})
	}

	for key, value := range tagFilters {
		myFilters = append(myFilters, &ec2.Filter{
			Name: aws.String(fmt.Sprintf("tag:%s", key)),
			Values: []*string{
				aws.String(value),
			},
		})
	}

	params := &ec2.DescribeInstancesInput{Filters: myFilters}

	resp, err := svc.DescribeInstances(params)
	if err != nil {
		return nil, err
	}

	var instances Instances
	for _, res := range resp.Reservations {
		for _, inst := range res.Instances {
			instance, err := GetInstance(inst)
			if err != nil {
				return nil, err
			}
			instances = append(instances, instance)
		}
	}

	sort.Sort(instances)

	return instances, nil
}

func Ec2Service(region string) (*ec2.EC2, error) {
	sess, err := session.NewSession()
	if err != nil {
		return nil, err
	}

	return ec2.New(sess, &aws.Config{Region: aws.String(region)}), nil
}

func main() {
	region := "us-east-1"

	svc, err := Ec2Service(region)
	if err != nil {
		log.Fatal(err)
	}

	filters := map[string]string{}
	tagFilters := map[string]string{}

	instances, err := GetInstances(svc, filters, tagFilters)
	if err != nil {
		log.Fatal(err)
	}

	name := "testApp"

	startMarker := fmt.Sprintf("# START EC2HOSTS - %s #", name)
	endMarker := fmt.Sprintf("# END EC2HOSTS - %s #", name)

	content := []string{}

	inFile := "/etc/hosts"
	public := "ansible"

	var fr io.ReadCloser
	fr, err = os.OpenFile(inFile, os.O_RDONLY, 0644)

	var state State = Before

	scanner := bufio.NewScanner(fr)
	for scanner.Scan() {
		line := scanner.Text()
		if line == startMarker {
			if state == Before {
				content = append(content, startMarker)
				state = Inside
				for _, inst := range instances {
					var ip string
					if strings.Contains(inst.Name, public) {
						ip = inst.PublicIpAddress
					} else {
						ip = inst.PrivateIpAddress
					}
					content = append(content, fmt.Sprintf("%s %s", ip, inst.Name))
				}
			} else {
				log.Fatal("Invalid marker - start")
			}
		} else if line == endMarker {
			if state == Inside {
				content = append(content, endMarker)
				state = After
			} else {
				log.Fatal("Invalid marker - end")
			}
		} else if state == Before || state == After {
			content = append(content, line)
		}
	}
	if err := scanner.Err(); err != nil {
		log.Fatal(err)
	}
	fr.Close()

	if state == Before {
		// no markers found
		content = append(content, "", startMarker)
		for _, inst := range instances {
			var ip string
			if strings.Contains(inst.Name, public) {
				ip = inst.PublicIpAddress
			} else {
				ip = inst.PrivateIpAddress
			}
			content = append(content, fmt.Sprintf("%s %s", ip, inst.Name))
		}
		content = append(content, endMarker)
	}

	var fw io.WriteCloser

	outFile := ""

	if outFile == "" {
		fw = os.Stdout
	} else {
		fw, err = os.OpenFile(outFile, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0644)
		if err != nil {
			log.Fatal(err)
		}
		defer fw.Close()
	}

	_, err = fw.Write([]byte(strings.Join(content, "\n")))
	if err != nil {
		log.Fatal(err)
	}

	_, err = fw.Write([]byte("\n"))
	if err != nil {
		log.Fatal(err)
	}
}
