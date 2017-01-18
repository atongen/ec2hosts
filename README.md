# ec2hosts

Queries AWS EC2 api and updates your local host file using instance name tag and public/private IP address.

Intended for use with [sshuttle](https://github.com/sshuttle/sshuttle) or ssh ProxyCommand.

Note: This is written in go. There is a similar ruby gem called [ec2_hosts](https://github.com/atongen/ec2_hosts).

## install

Download the [latest release](https://github.com/atongen/ec2hosts/releases), extract it,
and put it somewhere on your PATH.

or

```sh
λ go get github.com/atongen/ec2hosts
```

or

```sh
λ mkdir -p $GOPATH/src/github.com/atongen
λ cd $GOPATH/src/github.com/atongen
λ git clone https://github.com/atongen/ec2hosts.git
λ cd ec2hosts
λ go install
λ rehash
```

## cli options

```sh
λ ec2hosts -h
Usage of ec2hosts:
  -action string
        Action to perform: 'update', 'delete', or 'delete-all' (default "update")
  -backup
        Backup content of file before updating (default true)
  -dry-run
        Print updated file content to stdout only
  -file string
        Path to file to update (default "/etc/hosts")
  -name string
        Name of block of hosts in file
  -public string
        Pattern to use to match public hosts
  -region string
        AWS Region (default "us-east-1")
  -tag value
        Add instance tag filters, should be of the form -tag 'key:value'
  -v    Print version information and exit
  -vpc-id string
        Filter EC2 instances by vpc-id
```

## example

```
λ ec2hosts -name my-app -dry-run -public bastion
127.0.0.1 localhost

# The following lines are desirable for IPv6 capable hosts
::1     ip6-localhost ip6-loopback
fe00::0 ip6-localnet
ff00::0 ip6-mcastprefix
ff02::1 ip6-allnodes
ff02::2 ip6-allrouters

# START EC2HOSTS - my-app #
55.55.55.55 my-app-bastion # i-xxxxxxxx t2.micro
172.60.0.125 my-app-production-web # i-xxxxxxxx c4.xlarge
172.60.0.126 my-app-production-job # i-xxxxxxxx c4.xlarge
172.60.0.127 my-app-production-cron # i-xxxxxxxx c4.xlarge
172.60.0.128 my-app-production-db # i-xxxxxxxx c4.xlarge
# END EC2HOSTS - my-app #
```

## Contributing

Bug reports and pull requests are welcome on GitHub at [https://github.com/atongen/ec2hosts](https://github.com/atongen/ec2hosts).
