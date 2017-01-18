package main

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"regexp"
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
	After
	Inside
	Outside
)

var (
	StartMarkerRe = regexp.MustCompile(`^# START EC2HOSTS - .+ #$`)
	EndMarkerRe   = regexp.MustCompile(`^# END EC2HOSTS - .+ #$`)
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

func StartMarker(name string) string {
	return fmt.Sprintf("# START EC2HOSTS - %s #", name)
}

func EndMarker(name string) string {
	return fmt.Sprintf("# END EC2HOSTS - %s #", name)
}

func Update(input io.Reader, instances Instances, name, public string) ([]byte, error) {
	var (
		content bytes.Buffer
		err     error
	)

	state := Before
	startMarker := StartMarker(name)
	endMarker := EndMarker(name)

	scanner := bufio.NewScanner(input)
	for scanner.Scan() {
		line := scanner.Text()
		if line == startMarker {
			if state == Before {
				_, err = fmt.Fprintf(&content, "%s\n", startMarker)
				if err != nil {
					return content.Bytes(), err
				}
				state = Inside
				for _, inst := range instances {
					var ip string
					if public != "" && strings.Contains(inst.Name, public) {
						ip = inst.PublicIpAddress
					} else {
						ip = inst.PrivateIpAddress
					}
					_, err = fmt.Fprintf(&content, "%s %s # %s %s\n", ip, inst.Name, inst.Id, inst.Type)
					if err != nil {
						return content.Bytes(), err
					}
				}
			} else {
				return content.Bytes(), errors.New("Invalid start marker")
			}
		} else if line == endMarker {
			if state == Inside {
				_, err = fmt.Fprintf(&content, "%s\n", endMarker)
				if err != nil {
					return content.Bytes(), err
				}
				state = After
			} else {
				return content.Bytes(), errors.New("Invalid end marker")
			}
		} else if state == Before || state == After {
			_, err = fmt.Fprintf(&content, "%s\n", line)
			if err != nil {
				return content.Bytes(), err
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return content.Bytes(), err
	}

	if state == Before {
		// no markers found
		_, err = fmt.Fprintf(&content, "\n%s\n", startMarker)
		if err != nil {
			return content.Bytes(), err
		}
		for _, inst := range instances {
			var ip string
			if public != "" && strings.Contains(inst.Name, public) {
				ip = inst.PublicIpAddress
			} else {
				ip = inst.PrivateIpAddress
			}
			_, err = fmt.Fprintf(&content, "%s %s # %s %s\n", ip, inst.Name, inst.Id, inst.Type)
			if err != nil {
				return content.Bytes(), err
			}
		}
		_, err = fmt.Fprintf(&content, "%s\n", endMarker)
		if err != nil {
			return content.Bytes(), err
		}
	}

	return content.Bytes(), nil
}

func Delete(input io.Reader, name string) ([]byte, error) {
	var (
		content bytes.Buffer
		err     error
	)

	state := Outside
	startMarker := StartMarker(name)
	endMarker := EndMarker(name)

	scanner := bufio.NewScanner(input)
	for scanner.Scan() {
		line := scanner.Text()
		if line == startMarker {
			if state == Outside {
				state = Inside
			} else {
				return content.Bytes(), errors.New("Invalid start marker")
			}
		} else if line == endMarker {
			if state == Inside {
				state = Outside
			} else {
				return content.Bytes(), errors.New("Invalid end marker")
			}
		} else if state == Outside {
			_, err = fmt.Fprintf(&content, "%s\n", line)
			if err != nil {
				return content.Bytes(), err
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return content.Bytes(), err
	}

	return content.Bytes(), nil
}

func DeleteAll(input io.Reader) ([]byte, error) {
	var (
		content bytes.Buffer
		err     error
	)

	state := Outside

	scanner := bufio.NewScanner(input)
	for scanner.Scan() {
		line := scanner.Text()
		if StartMarkerRe.MatchString(line) {
			if state == Outside {
				state = Inside
			} else {
				return content.Bytes(), errors.New("Invalid start marker")
			}
		} else if EndMarkerRe.MatchString(line) {
			if state == Inside {
				state = Outside
			} else {
				return content.Bytes(), errors.New("Invalid end marker")
			}
		} else if state == Outside {
			_, err = fmt.Fprintf(&content, "%s\n", line)
			if err != nil {
				return content.Bytes(), err
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return content.Bytes(), err
	}

	return content.Bytes(), nil
}

func WriteFile(file string, content []byte) error {
	fw, err := os.OpenFile(file, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0644)
	if err != nil {
		return err
	}
	defer fw.Close()

	_, err = fw.Write(content)
	return err
}
