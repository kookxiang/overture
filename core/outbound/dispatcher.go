package outbound

import (
	"context"
	"math/rand"
	"net"
	"os"
	"time"

	"github.com/miekg/dns"
	"github.com/shawn1m/overture/core/outbound/clients/resolver"
	log "github.com/sirupsen/logrus"

	"github.com/shawn1m/overture/core/cache"
	"github.com/shawn1m/overture/core/common"
	"github.com/shawn1m/overture/core/hosts"
	"github.com/shawn1m/overture/core/matcher"
	"github.com/shawn1m/overture/core/outbound/clients"
)

type Dispatcher struct {
	PrimaryDNS     []*common.DNSUpstream
	AlternativeDNS []*common.DNSUpstream
	BootstrapDNS   []string
	OnlyPrimaryDNS bool

	WhenPrimaryDNSAnswerNoneUse string
	IPNetworkPrimarySet         *common.IPSet
	IPNetworkAlternativeSet     *common.IPSet
	DomainPrimaryList           matcher.Matcher
	DomainAlternativeList       matcher.Matcher
	RedirectIPv6Record          bool
	AlternativeDNSConcurrent    bool

	MinimumTTL   int
	DomainTTLMap map[string]uint32

	Hosts *hosts.Hosts
	Cache *cache.Cache

	primaryResolvers     []resolver.Resolver
	alternativeResolvers []resolver.Resolver
	bootstrapResolver    *net.Resolver
}

func createResolver(br *net.Resolver, ul []*common.DNSUpstream) (resolvers []resolver.Resolver) {
	resolvers = make([]resolver.Resolver, len(ul))
	for i, u := range ul {
		u.BootstrapResolver = br
		resolvers[i] = resolver.NewResolver(u)
	}
	return resolvers
}

func createBootstrapResolver(providers []string) *net.Resolver {
	if len(providers) == 0 {
		return nil
	}
	return &net.Resolver{
		PreferGo: true,
		Dial: func(ctx context.Context, network, address string) (net.Conn, error) {
			dialer := &net.Dialer{
				Timeout: time.Second * 3,
			}
			serverAddr := providers[rand.Intn(len(providers))]
			if net.ParseIP(serverAddr) != nil {
				return dialer.DialContext(ctx, network, net.JoinHostPort(serverAddr, "53"))
			} else if host, port, err := net.SplitHostPort(serverAddr); err == nil {
				return dialer.DialContext(ctx, network, net.JoinHostPort(host, port))
			} else {
				return nil, os.ErrInvalid
			}
		},
	}
}

func (d *Dispatcher) Init() {
	var bootstrapResolver *net.Resolver
	if len(d.BootstrapDNS) > 0 {
		bootstrapResolver = createBootstrapResolver(d.BootstrapDNS)
	}
	d.primaryResolvers = createResolver(bootstrapResolver, d.PrimaryDNS)
	d.alternativeResolvers = createResolver(bootstrapResolver, d.AlternativeDNS)
}

func (d *Dispatcher) Exchange(query *dns.Msg, inboundIP string) *dns.Msg {
	PrimaryClientBundle := clients.NewClientBundle(query, d.PrimaryDNS, d.primaryResolvers, inboundIP, d.MinimumTTL, d.Cache, "Primary", d.DomainTTLMap)
	AlternativeClientBundle := clients.NewClientBundle(query, d.AlternativeDNS, d.alternativeResolvers, inboundIP, d.MinimumTTL, d.Cache, "Alternative", d.DomainTTLMap)

	var ActiveClientBundle *clients.RemoteClientBundle

	localClient := clients.NewLocalClient(query, d.Hosts, d.MinimumTTL, d.DomainTTLMap)
	resp := localClient.Exchange()
	if resp != nil {
		return resp
	}

	for _, cb := range []*clients.RemoteClientBundle{PrimaryClientBundle, AlternativeClientBundle} {
		resp := cb.ExchangeFromCache()
		if resp != nil {
			return resp
		}
	}

	if d.OnlyPrimaryDNS || d.isSelectDomain(PrimaryClientBundle, d.DomainPrimaryList) {
		ActiveClientBundle = PrimaryClientBundle
		return ActiveClientBundle.Exchange(true, true)
	}

	if ok := d.isExchangeForIPv6(query) || d.isSelectDomain(AlternativeClientBundle, d.DomainAlternativeList); ok {
		ActiveClientBundle = AlternativeClientBundle
		return ActiveClientBundle.Exchange(true, true)
	}

	ActiveClientBundle = d.selectByIPNetwork(PrimaryClientBundle, AlternativeClientBundle)

	// Only try to Cache result before return
	ActiveClientBundle.CacheResultIfNeeded()
	return ActiveClientBundle.GetResponseMessage()
}

func (d *Dispatcher) isExchangeForIPv6(query *dns.Msg) bool {
	if query.Question[0].Qtype == dns.TypeAAAA && d.RedirectIPv6Record {
		log.Debug("Finally use alternative DNS")
		return true
	}

	return false
}

func (d *Dispatcher) isSelectDomain(rcb *clients.RemoteClientBundle, dt matcher.Matcher) bool {
	if dt != nil {
		qn := rcb.GetFirstQuestionDomain()

		if dt.Has(qn) {
			log.WithFields(log.Fields{
				"DNS":      rcb.Name,
				"question": qn,
				"domain":   qn,
			}).Debug("Matched")
			log.Debugf("Finally use %s DNS", rcb.Name)
			return true
		}

		log.Debugf("Domain %s match fail", rcb.Name)
	} else {
		log.Debug("Domain matcher is nil, not checking")
	}

	return false
}

func (d *Dispatcher) selectByIPNetwork(PrimaryClientBundle, AlternativeClientBundle *clients.RemoteClientBundle) *clients.RemoteClientBundle {
	primaryOut := make(chan *dns.Msg)
	alternateOut := make(chan *dns.Msg)
	go func() {
		primaryOut <- PrimaryClientBundle.Exchange(false, true)
	}()
	alternateFunc := func() {
		alternateOut <- AlternativeClientBundle.Exchange(false, true)
	}
	waitAlternateResp := func() {
		if !d.AlternativeDNSConcurrent {
			go alternateFunc()
		}
		<-alternateOut
	}
	if d.AlternativeDNSConcurrent {
		go alternateFunc()
	}
	primaryResponse := <-primaryOut

	if primaryResponse != nil {
		if primaryResponse.Answer == nil {
			if d.WhenPrimaryDNSAnswerNoneUse != "alternativeDNS" && d.WhenPrimaryDNSAnswerNoneUse != "AlternativeDNS" {
				log.Debug("primaryDNS response has no answer section but exist, finally use primaryDNS")
				return PrimaryClientBundle
			} else {
				log.Debug("primaryDNS response has no answer section but exist, finally use alternativeDNS")
				waitAlternateResp()
				return AlternativeClientBundle
			}
		}
	} else {
		log.Debug("Primary DNS return nil, finally use alternative DNS")
		waitAlternateResp()
		return AlternativeClientBundle
	}

	for _, a := range PrimaryClientBundle.GetResponseMessage().Answer {
		log.Debug("Try to match response ip address with IP network")
		var ip net.IP
		if a.Header().Rrtype == dns.TypeA {
			ip = net.ParseIP(a.(*dns.A).A.String())
		} else if a.Header().Rrtype == dns.TypeAAAA {
			ip = net.ParseIP(a.(*dns.AAAA).AAAA.String())
		} else {
			continue
		}
		if d.IPNetworkPrimarySet.Contains(ip, true, "primary") {
			log.Debug("Finally use primary DNS")
			return PrimaryClientBundle
		}
		if d.IPNetworkAlternativeSet.Contains(ip, true, "alternative") {
			log.Debug("Finally use alternative DNS")
			waitAlternateResp()
			return AlternativeClientBundle
		}
	}
	log.Debug("IP network match failed, finally use alternative DNS")
	waitAlternateResp()
	return AlternativeClientBundle
}
