package main

import (
	"bytes"
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"strings"
	"time"
)

// build flags
var (
	Version   string = "unset"
	BuildTime string = "unset"
	BuildUser string = "unset"
	BuildHash string = "unset"
)

type StrFlags []string

func (s *StrFlags) String() string {
	return strings.Join(*s, ", ")
}

func (s *StrFlags) Set(value string) error {
	*s = append(*s, value)
	return nil
}

// cli flags
var (
	versionFlag = flag.Bool("v", false, "Print version information and exit")
	actionFlag  = flag.String("action", "update", "Action to perform: 'update', 'delete', or 'delete-all'")
	nameFlag    = flag.String("name", "", "Name of block of hosts in file")
	regionFlag  = flag.String("region", "us-east-1", "AWS Region")
	vpcIdFlag   = flag.String("vpc-id", "", "Filter EC2 instances by vpc-id")
	fileFlag    = flag.String("file", "/etc/hosts", "Path to file to update")
	publicFlag  = flag.String("public", "", "Pattern to use to match public hosts")
	dryRunFlag  = flag.Bool("dry-run", false, "Print updated file content to stdout only")
	backupFlag  = flag.Bool("backup", true, "Backup content of file before updating")
	tagFlags    StrFlags
)

func init() {
	flag.Var(&tagFlags, "tag", "Add instance tag filters, should be of the form -tag 'key:value'")
}

func main() {
	flag.Parse()

	if *versionFlag {
		fmt.Printf("ec2hosts %s %s %s %s\n", Version, BuildTime, BuildUser, BuildHash)
		os.Exit(0)
	}

	if *actionFlag != "update" && *actionFlag != "delete" && *actionFlag != "delete-all" {
		log.Fatal("action must be 'update', 'delete', or 'delete-all'")
	}

	if *actionFlag != "delete-all" && *nameFlag == "" {
		log.Fatal("name is required")
	}

	filters := map[string]string{}
	if *vpcIdFlag != "" {
		filters["vpc-id"] = *vpcIdFlag
	}

	tagFilters := map[string]string{}
	for _, tag := range tagFlags {
		vals := strings.Split(tag, ":")
		if len(vals) != 2 {
			log.Fatalf("Invalid tag filter: %s\n", tag)
		}
		tagFilters[vals[0]] = vals[1]
	}

	svc, err := Ec2Service(*regionFlag)
	if err != nil {
		log.Fatal(err)
	}

	instances, err := GetInstances(svc, filters, tagFilters)
	if err != nil {
		log.Fatal(err)
	}

	fr, err := os.OpenFile(*fileFlag, os.O_RDONLY, 0644)
	if err != nil {
		log.Fatal(err)
	}

	var content []byte
	switch *actionFlag {
	default:
		log.Fatalf("Unknown action: %s\n", *actionFlag)
	case "update":
		content, err = Update(fr, instances, *nameFlag, *publicFlag)
	case "delete":
		content, err = Delete(fr, *nameFlag)
	case "delete-all":
		content, err = DeleteAll(fr)
	}
	if err != nil {
		log.Fatal(err)
	}

	if *dryRunFlag {
		fmt.Print(string(content))
		os.Exit(0)
	}

	_, err = fr.Seek(0, 0)
	if err != nil {
		log.Fatal(err)
	}

	originalContent, err := ioutil.ReadAll(fr)
	if err != nil {
		log.Fatal(err)
	}

	fr.Close()

	if bytes.Equal(originalContent, content) {
		os.Exit(0)
	}

	if *backupFlag {
		bakFile := fmt.Sprintf("%s.%d", *fileFlag, time.Now().Unix())
		err = WriteFile(bakFile, originalContent)
		if err != nil {
			log.Fatal(err)
		}
	}

	err = WriteFile(*fileFlag, content)
	if err != nil {
		log.Fatal(err)
	}
}
