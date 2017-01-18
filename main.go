package main

import (
	"bytes"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"time"
)

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

	action := "update"
	name := "testApp"
	file := "/etc/hosts"
	public := "ansible"
	dryRun := false
	backup := true

	fr, err := os.OpenFile(file, os.O_RDONLY, 0644)
	if err != nil {
		log.Fatal(err)
	}

	var content []byte
	switch action {
	default:
		log.Fatalf("Unknown action: %s\n", action)
	case "update":
		content, err = Update(fr, instances, name, public)
	case "delete":
		content, err = Delete(fr, name)
	case "delete-all":
		content, err = DeleteAll(fr)
	}
	if err != nil {
		log.Fatal(err)
	}

	if dryRun {
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

	if backup {
		bakFile := fmt.Sprintf("%s.%d", file, time.Now().Unix())
		err = WriteFile(bakFile, originalContent)
	}

	err = WriteFile(file, content)
	if err != nil {
		log.Fatal(err)
	}
}
