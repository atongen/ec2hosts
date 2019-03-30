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
	"github.com/aws/aws-sdk-go/aws/ec2metadata"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/ec2"
)

type Instance struct {
	i *ec2.Instance
}

type Instances []Instance

func (s Instances) Len() int {
	return len(s)
}

func (s Instances) Less(i, j int) bool {
	return s[i].Name() < s[j].Name()
}

func (s Instances) Swap(i, j int) {
	s[i], s[j] = s[j], s[i]
}

func (i Instance) Name() string {
	name := i.Tag("Name")
	if name == "" {
		return i.Id()
	}
	return name
}

func (i Instance) Id() string {
	return aws.StringValue(i.i.InstanceId)
}

func (i Instance) Tag(t string) string {
	for _, keys := range i.i.Tags {
		if *keys.Key == t {
			return url.QueryEscape(*keys.Value)
		}
	}
	return ""
}

func (i Instance) Type() string {
	return aws.StringValue(i.i.InstanceType)
}

func (i Instance) PrivateIpAddress() string {
	return aws.StringValue(i.i.PrivateIpAddress)
}

func (i Instance) PublicIpAddress() string {
	return aws.StringValue(i.i.PublicIpAddress)
}

func (i Instance) AvailabilityZone() string {
	if i.i.Placement != nil {
		return aws.StringValue(i.i.Placement.AvailabilityZone)
	}
	return ""
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
	HostRe        = regexp.MustCompile(`^[A-Za-z0-9-]+$`)
)

func GetInstances(svc *ec2.EC2, filters map[string]string, tagFilters map[string]string, exclude string) (Instances, error) {
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

	var (
		instances Instances
		nextToken *string
	)

	for {
		if nextToken != nil {
			params.NextToken = nextToken
		}

		resp, err := svc.DescribeInstances(params)
		if err != nil {
			return nil, err
		}

		nextToken = resp.NextToken

		for _, res := range resp.Reservations {
			for _, inst := range res.Instances {
				instance := Instance{inst}
				if HostRe.MatchString(instance.Name()) {
					if exclude == "" || !strings.Contains(instance.Name(), exclude) {
						instances = append(instances, instance)
					}
				}
			}
		}

		if nextToken == nil {
			break
		}
	}

	sort.Sort(instances)

	return instances, nil
}

// getRegion deduces the current AWS Region.
func getRegion(sess *session.Session) (string, error) {
	region, present := os.LookupEnv("AWS_REGION")
	if !present {
		region, present = os.LookupEnv("AWS_DEFAULT_REGION")
	}
	if !present {
		svc := ec2metadata.New(sess)
		return svc.Region()
	}
	return region, nil
}

func Ec2Service(region string) (*ec2.EC2, error) {
	sess, err := session.NewSession()
	if err != nil {
		return nil, err
	}

	if region == "" {
		region, err = getRegion(sess)
		if err != nil {
			return nil, err
		}
	}

	return ec2.New(sess, &aws.Config{Region: aws.String(region)}), nil
}

func StartMarker(name string) string {
	return fmt.Sprintf("# START EC2HOSTS - %s #", name)
}

func EndMarker(name string) string {
	return fmt.Sprintf("# END EC2HOSTS - %s #", name)
}

func writeInstanceContent(w io.Writer, ip string, i Instance, tags []string) error {
	f := []string{"%s %s # %s %s %s"}
	a := []interface{}{ip, i.Name(), i.Id(), i.Type(), i.AvailabilityZone()}

	for _, key := range tags {
		value := i.Tag(key)
		if value != "" {
			f = append(f, "%s")
			a = append(a, value)
		}
	}

	_, err := fmt.Fprintf(w, strings.Join(f, " ")+"\n", a...)
	return err
}

func Update(input io.Reader, instances Instances, name, public string, tags []string) ([]byte, error) {
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
					if public != "" && strings.Contains(inst.Name(), public) {
						ip = inst.PublicIpAddress()
					} else {
						ip = inst.PrivateIpAddress()
					}
					err = writeInstanceContent(&content, ip, inst, tags)
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
			if public != "" && strings.Contains(inst.Name(), public) {
				ip = inst.PublicIpAddress()
			} else {
				ip = inst.PrivateIpAddress()
			}
			err = writeInstanceContent(&content, ip, inst, tags)
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
