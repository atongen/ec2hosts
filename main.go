package main

import (
	"fmt"
	"log"
	"net/url"
	"sort"

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

func GetInstances(svc *ec2.EC2) (Instances, error) {
	params := &ec2.DescribeInstancesInput{
		Filters: []*ec2.Filter{
			&ec2.Filter{
				Name: aws.String("instance-state-name"),
				Values: []*string{
					aws.String("running"),
				},
			},
		},
	}

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
	svc, err := Ec2Service("us-east-1")
	if err != nil {
		log.Fatal(err)
	}

	instances, err := GetInstances(svc)
	if err != nil {
		log.Fatal(err)
	}

	for _, inst := range instances {
		fmt.Println(inst)
	}
}
