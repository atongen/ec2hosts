package main

import (
	"bytes"
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

// build flags
var (
	Version   string = "development"
	BuildTime string = "unset"
	BuildHash string = "unset"
	GoVersion string = "unset"
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
	excludeFlag = flag.String("exclude", "", "Pattern of hostname to exclude")
	dryRunFlag  = flag.Bool("dry-run", false, "Print updated file content to stdout only")
	backupFlag  = flag.Int("backup", 3, "Number of backup files to keep, 0 is no backup")
	tagFlags    StrFlags
	tagOutFlags StrFlags
)

func init() {
	flag.Var(&tagFlags, "tag", "Add instance tag filters, should be of the form -tag 'key:value'")
	flag.Var(&tagOutFlags, "tag-out", "Include value for tag in host file output, should be of the form -tag 'key'")
}

func versionStr() string {
	return fmt.Sprintf("%s %s %s %s %s", path.Base(os.Args[0]), Version, BuildTime, BuildHash, GoVersion)
}

func getBackups(basePath string) ([]string, error) {
	backups := []string{}

	dir, file := filepath.Split(basePath)
	if dir == "" || file == "" {
		return backups, fmt.Errorf("Invalid basepath")
	}

	files, err := ioutil.ReadDir(dir)
	if err != nil {
		return backups, err
	}

	for _, f := range files {
		s := path.Join(dir, f.Name())
		matched, err := regexp.MatchString("^"+basePath+`\.\d+$`, s)
		if err != nil {
			log.Printf("Error matching for cleanup: %s", s)
			continue
		}

		if matched {
			backups = append(backups, s)
		}
	}

	return backups, nil
}

func cleanupBackups(basePath string, numBackups int) error {
	backups, err := getBackups(basePath)
	if err != nil {
		return err
	}

	if len(backups) <= numBackups {
		return nil
	}

	sort.Strings(backups)
	for i := 0; i < len(backups)-numBackups; i++ {
		err = os.Remove(backups[i])
		if err != nil {
			log.Printf("Error deleting %s", backups[i])
		}
	}

	return nil
}

func main() {
	flag.Parse()

	if *versionFlag {
		fmt.Println(versionStr())
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

	instances, err := GetInstances(svc, filters, tagFilters, *excludeFlag)
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
		content, err = Update(fr, instances, *nameFlag, *publicFlag, tagOutFlags)
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

	if *backupFlag > 0 {
		bakFile := fmt.Sprintf("%s.%d", *fileFlag, time.Now().Unix())
		err = WriteFile(bakFile, originalContent)
		if err != nil {
			log.Fatal(err)
		}
		err = cleanupBackups(*fileFlag, *backupFlag)
		if err != nil {
			log.Printf("Error cleaning up old backup files: %s", err)
		}
	}

	err = WriteFile(*fileFlag, content)
	if err != nil {
		log.Fatal(err)
	}
}
