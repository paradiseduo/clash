package executor

import (
	"fmt"
	"io/ioutil"
	"os"
	"sync"

	"github.com/paradiseduo/clashr/adapters/provider"
	"github.com/paradiseduo/clashr/component/auth"
	"github.com/paradiseduo/clashr/component/dialer"
	"github.com/paradiseduo/clashr/component/trie"
	"github.com/paradiseduo/clashr/component/resolver"
	"github.com/paradiseduo/clashr/config"
	C "github.com/paradiseduo/clashr/constant"
	"github.com/paradiseduo/clashr/dns"
	"github.com/paradiseduo/clashr/log"
	P "github.com/paradiseduo/clashr/proxy"
	authStore "github.com/paradiseduo/clashr/proxy/auth"
	"github.com/paradiseduo/clashr/tunnel"
)

var (
	mux sync.Mutex
)

func readConfig(path string) ([]byte, error) {
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return nil, err
	}
	data, err := ioutil.ReadFile(path)
	if err != nil {
		return nil, err
	}

	if len(data) == 0 {
		return nil, fmt.Errorf("configuration file %s is empty", path)
	}

	return data, err
}

// Parse config with default config path
func Parse() (*config.Config, error) {
	return ParseWithPath(C.Path.Config())
}

// ParseWithPath parse config with custom config path
func ParseWithPath(path string) (*config.Config, error) {
	buf, err := readConfig(path)
	if err != nil {
		return nil, err
	}

	return ParseWithBytes(buf)
}

// ParseWithBytes config with buffer
func ParseWithBytes(buf []byte) (*config.Config, error) {
	return config.Parse(buf)
}

// ApplyConfig dispatch configure to all parts
func ApplyConfig(cfg *config.Config, force bool) {
	mux.Lock()
	defer mux.Unlock()

	updateUsers(cfg.Users)
	updateGeneral(cfg.General, force)
	updateProxies(cfg.Proxies, cfg.Providers)
	updateRules(cfg.Rules)
	updateDNS(cfg.DNS)
	updateHosts(cfg.Hosts)
	updateExperimental(cfg)
}

func GetGeneral() *config.General {
	ports := P.GetPorts()
	authenticator := []string{}
	if auth := authStore.Authenticator(); auth != nil {
		authenticator = auth.Users()
	}

	general := &config.General{
		Inbound: config.Inbound{
			Port:           ports.Port,
			SocksPort:      ports.SocksPort,
			RedirPort:      ports.RedirPort,
			MixedPort:      ports.MixedPort,
			Authentication: authenticator,
			AllowLan:       P.AllowLan(),
			BindAddress:    P.BindAddress(),
		},
		Mode:     tunnel.Mode(),
		LogLevel: log.Level(),
	}

	return general
}

func updateExperimental(c *config.Config) {}

func updateDNS(c *config.DNS) {
	if !c.Enable {
		resolver.DefaultResolver = nil
		tunnel.SetResolver(nil)
		dns.ReCreateServer("", nil)
		return
	}
	r := dns.New(dns.Config{
		Main:         c.NameServer,
		Fallback:     c.Fallback,
		IPv6:         c.IPv6,
		EnhancedMode: c.EnhancedMode,
		Pool:         c.FakeIPRange,
		Hosts:        c.Hosts,
		FallbackFilter: dns.FallbackFilter{
			GeoIP:  c.FallbackFilter.GeoIP,
			IPCIDR: c.FallbackFilter.IPCIDR,
		},
		Default: c.DefaultNameserver,
	})

	// reuse cache of old resolver
	if resolver.DefaultResolver != nil {
		if o, ok := resolver.DefaultResolver.(*dns.Resolver); ok {
			o.PatchCache(r)
		}
	}

	resolver.DefaultResolver = r
	tunnel.SetResolver(r)
	if err := dns.ReCreateServer(c.Listen, r); err != nil {
		log.Errorln("Start DNS server error: %s", err.Error())
		return
	}

	if c.Listen != "" {
		log.Infoln("DNS server listening at: %s", c.Listen)
	}
}

func updateHosts(tree *trie.DomainTrie) {
	resolver.DefaultHosts = tree
}

func updateProxies(proxies map[string]C.Proxy, providers map[string]provider.ProxyProvider) {
	tunnel.UpdateProxies(proxies, providers)
}

func updateRules(rules []C.Rule) {
	tunnel.UpdateRules(rules)
}

func updateGeneral(general *config.General, force bool) {
	log.SetLevel(general.LogLevel)
	tunnel.SetMode(general.Mode)
	resolver.DisableIPv6 = !general.IPv6

	if general.Interface != "" {
		dialer.DialHook = dialer.DialerWithInterface(general.Interface)
		dialer.ListenPacketHook = dialer.ListenPacketWithInterface(general.Interface)
	} else {
		dialer.DialHook = nil
		dialer.ListenPacketHook = nil
	}

	if !force {
		return
	}

	allowLan := general.AllowLan
	P.SetAllowLan(allowLan)

	bindAddress := general.BindAddress
	P.SetBindAddress(bindAddress)

	if err := P.ReCreateHTTP(general.Port); err != nil {
		log.Errorln("Start HTTP server error: %s", err.Error())
	}

	if err := P.ReCreateSocks(general.SocksPort); err != nil {
		log.Errorln("Start SOCKS5 server error: %s", err.Error())
	}

	if err := P.ReCreateRedir(general.RedirPort); err != nil {
		log.Errorln("Start Redir server error: %s", err.Error())
	}

	if err := P.ReCreateMixed(general.MixedPort); err != nil {
		log.Errorln("Start Mixed(http and socks5) server error: %s", err.Error())
	}
}

func updateUsers(users []auth.AuthUser) {
	authenticator := auth.NewAuthenticator(users)
	authStore.SetAuthenticator(authenticator)
	if authenticator != nil {
		log.Infoln("Authentication of local server updated")
	}
}
