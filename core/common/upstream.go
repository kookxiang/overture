package common

import (
	"net"
)

type DNSUpstream struct {
	Name             string                `yaml:"name" json:"name"`
	Address          string                `yaml:"address" json:"address"`
	Protocol         string                `yaml:"protocol" json:"protocol"`
	SOCKS5Address    string                `yaml:"socks5Address" json:"socks5Address"`
	Timeout          int                   `yaml:"timeout" json:"timeout"`
	EDNSClientSubnet *EDNSClientSubnetType `yaml:"ednsClientSubnet" json:"ednsClientSubnet"`
	TCPPoolConfig    struct {
		Enable          bool `yaml:"enable" json:"enable"`
		InitialCapacity int  `yaml:"initialCapacity" json:"initialCapacity"`
		MaxCapacity     int  `yaml:"maxCapacity" json:"maxCapacity"`
		IdleTimeout     int  `yaml:"idleTimeout" json:"idleTimeout"`
	} `yaml:"tcpPoolConfig" json:"tcpPoolConfig"`

	BootstrapResolver *net.Resolver `yaml:"-" json:"-"`
}

func (u *DNSUpstream) Initialize() {
	if u.Protocol == "" {
		u.Protocol = "udp"
	}

	if u.Timeout == 0 {
		u.Timeout = 5
	}

	if u.EDNSClientSubnet == nil {
		u.EDNSClientSubnet = &EDNSClientSubnetType{
			Policy:     "auto",
			ExternalIP: "",
			NoCookie:   false,
		}
	}
}
