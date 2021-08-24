package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"strings"

	extapi "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1beta1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"

	"github.com/jetstack/cert-manager/pkg/acme/webhook/apis/acme/v1alpha1"
	"github.com/jetstack/cert-manager/pkg/acme/webhook/cmd"
	cmmeta "github.com/jetstack/cert-manager/pkg/apis/meta/v1"
	"github.com/jetstack/cert-manager/pkg/issuer/acme/dns/util"

	namecheap "github.com/namecheap/go-namecheap-sdk/v2/namecheap"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var GroupName = os.Getenv("GROUP_NAME")

func main() {
	if GroupName == "" {
		panic("GROUP_NAME must be specified")
	}

	// This will register our namecheap DNS provider with the webhook serving
	// library, making it available as an API under the provided GroupName.
	// You can register multiple DNS provider implementations with a single
	// webhook, where the Name() method will be used to disambiguate between
	// the different implementations.
	cmd.RunWebhookServer(GroupName,
		&namecheapDNSProviderSolver{},
	)
}

// namecheapDNSProviderSolver implements the provider-specific logic needed to
// 'present' an ACME challenge TXT record for your own DNS provider.
// To do so, it must implement the `github.com/jetstack/cert-manager/pkg/acme/webhook.Solver`
// interface.
type namecheapDNSProviderSolver struct {
	// If a Kubernetes 'clientset' is needed, you must:
	// 1. uncomment the additional `client` field in this structure below
	// 2. uncomment the "k8s.io/client-go/kubernetes" import at the top of the file
	// 3. uncomment the relevant code in the Initialize method below
	// 4. ensure your webhook's service account has the required RBAC role
	//    assigned to it for interacting with the Kubernetes APIs you need.
	ctx             context.Context
	k8sClient       *kubernetes.Clientset
	namecheapClient *namecheap.Client
}

// namecheapDNSProviderConfig is a structure that is used to decode into when
// solving a DNS01 challenge.
// This information is provided by cert-manager, and may be a reference to
// additional configuration that's needed to solve the challenge for this
// particular certificate or issuer.
// This typically includes references to Secret resources containing DNS
// provider credentials, in cases where a 'multi-tenant' DNS solver is being
// created.
// If you do *not* require per-issuer or per-certificate configuration to be
// provided to your webhook, you can skip decoding altogether in favour of
// using CLI flags or similar to provide configuration.
// You should not include sensitive information here. If credentials need to
// be used by your provider here, you should reference a Kubernetes Secret
// resource and fetch these credentials using a Kubernetes clientset.
type namecheapDNSProviderConfig struct {
	// These fields will be set by users in the
	// `issuer.spec.acme.dns01.providers.webhook.config` field.

	APIKeySecretRef cmmeta.SecretKeySelector `json:"apiKeySecretRef"`
	APIUser         string                   `json:"apiUser"`
	ClientIP        string                   `json:"clientIP"`
	UseSandbox      bool                     `json:"useSandbox"`
	Username        string                   `json:"username"`
}

// Name is used as the name for this DNS solver when referencing it on the ACME
// Issuer resource.
// This should be unique **within the group name**, i.e. you can have two
// solvers configured with the same Name() **so long as they do not co-exist
// within a single webhook deployment**.
// For example, `cloudflare` may be used as the name of a solver.
func (c *namecheapDNSProviderSolver) Name() string {
	return "namecheap"
}

// Present is responsible for actually presenting the DNS record with the
// DNS provider.
// This method should tolerate being called multiple times with the same value.
// cert-manager itself will later perform a self check to ensure that the
// solver has correctly configured the DNS provider.
func (c *namecheapDNSProviderSolver) Present(ch *v1alpha1.ChallengeRequest) error {
	cfg, err := loadConfig(ch.Config)
	if err != nil {
		return err
	}

	zone, domain, err := c.parseChallenge(ch)
	if err != nil {
		return err
	}

	if c.namecheapClient == nil {
		if err := c.setNamecheapClient(ch, cfg); err != nil {
			return err
		}
	}

	z, err := c.getZone(zone)
	if err != nil {
		return err
	}

	records, err := getRecords(z.Hosts)
	if err != nil {
		return err
	}

	records = addTestRecord(records, domain, ch.Key)

	args := &namecheap.DomainsDNSSetHostsArgs{
		Domain:    z.Domain,
		EmailType: z.EmailType,
		Records:   &records,
	}

	if _, err := c.namecheapClient.DomainsDNS.SetHosts(args); err != nil {
		return err
	}

	return nil
}

// CleanUp should delete the relevant TXT record from the DNS provider console.
// If multiple TXT records exist with the same record name (e.g.
// _acme-challenge.example.com) then **only** the record with the same `key`
// value provided on the ChallengeRequest should be cleaned up.
// This is in order to facilitate multiple DNS validations for the same domain
// concurrently.
func (c *namecheapDNSProviderSolver) CleanUp(ch *v1alpha1.ChallengeRequest) error {
	cfg, err := loadConfig(ch.Config)
	if err != nil {
		return err
	}

	zone, domain, err := c.parseChallenge(ch)
	if err != nil {
		return err
	}

	if c.namecheapClient == nil {
		if err := c.setNamecheapClient(ch, cfg); err != nil {
			return err
		}
	}

	z, err := c.getZone(zone)
	if err != nil {
		return err
	}

	records, err := getRecords(z.Hosts)
	if err != nil {
		return err
	}

	records = removeTestRecord(records, domain)

	args := &namecheap.DomainsDNSSetHostsArgs{
		Domain:    z.Domain,
		EmailType: z.EmailType,
		Records:   &records,
	}

	if _, err := c.namecheapClient.DomainsDNS.SetHosts(args); err != nil {
		return err
	}

	return nil
}

// Initialize will be called when the webhook first starts.
// This method can be used to instantiate the webhook, i.e. initialising
// connections or warming up caches.
// Typically, the kubeClientConfig parameter is used to build a Kubernetes
// client that can be used to fetch resources from the Kubernetes API, e.g.
// Secret resources containing credentials used to authenticate with DNS
// provider accounts.
// The stopCh can be used to handle early termination of the webhook, in cases
// where a SIGTERM or similar signal is sent to the webhook process.
func (c *namecheapDNSProviderSolver) Initialize(kubeClientConfig *rest.Config, stopCh <-chan struct{}) error {
	cl, err := kubernetes.NewForConfig(kubeClientConfig)
	if err != nil {
		return err
	}

	c.k8sClient = cl
	c.ctx = context.Background()

	return nil
}

// loadConfig is a small helper function that decodes JSON configuration into
// the typed config struct.
func loadConfig(cfgJSON *extapi.JSON) (namecheapDNSProviderConfig, error) {
	cfg := namecheapDNSProviderConfig{}
	// handle the 'base case' where no configuration has been provided
	if cfgJSON == nil {
		return cfg, nil
	}
	if err := json.Unmarshal(cfgJSON.Raw, &cfg); err != nil {
		return cfg, fmt.Errorf("error decoding solver config: %v", err)
	}

	return cfg, nil
}

func (c *namecheapDNSProviderSolver) setNamecheapClient(ch *v1alpha1.ChallengeRequest, cfg namecheapDNSProviderConfig) error {
	ref := cfg.APIKeySecretRef
	if ref.Name == "" {
		return fmt.Errorf(
			"secret for Namecheap apiKey not found in '%s'",
			ch.ResourceNamespace,
		)
	}
	if ref.Key == "" {
		return fmt.Errorf(
			"no 'key' set in secret '%s/%s'",
			ch.ResourceNamespace,
			ref.Name,
		)
	}

	secret, err := c.k8sClient.CoreV1().Secrets(ch.ResourceNamespace).Get(
		c.ctx, ref.Name, metav1.GetOptions{},
	)
	if err != nil {
		return err
	}
	apiKeyBytes, ok := secret.Data[ref.Key]
	if !ok {
		return fmt.Errorf(
			"no key '%s' in secret '%s/%s'",
			ref.Key,
			ch.ResourceNamespace,
			ref.Name,
		)
	}
	apiKey := string(apiKeyBytes)

	opts := &namecheap.ClientOptions{
		ApiKey:     apiKey,
		ApiUser:    cfg.APIUser,
		ClientIp:   cfg.ClientIP,
		UseSandbox: cfg.UseSandbox,
		UserName:   cfg.Username,
	}
	c.namecheapClient = namecheap.NewClient(
		opts,
	)

	return nil
}

func (c *namecheapDNSProviderSolver) getZone(zone string) (*namecheap.DomainDNSGetHostsResult, error) {
	resp, err := c.namecheapClient.DomainsDNS.GetHosts(zone)
	if err != nil {
		return nil, err
	}

	return resp.DomainDNSGetHostsResult, nil
}

// Get the zone and domain we are setting from the challenge request
// source: https://github.com/ns1/cert-manager-webhook-ns1
func (c *namecheapDNSProviderSolver) parseChallenge(ch *v1alpha1.ChallengeRequest) (
	zone string, domain string, err error,
) {

	if zone, err = util.FindZoneByFqdn(
		ch.ResolvedFQDN, util.RecursiveNameservers,
	); err != nil {
		return "", "", err
	}
	zone = util.UnFqdn(zone)

	if idx := strings.Index(ch.ResolvedFQDN, "."+ch.ResolvedZone); idx != -1 {
		domain = ch.ResolvedFQDN[:idx]
	} else {
		domain = util.UnFqdn(ch.ResolvedFQDN)
	}

	return zone, domain, nil
}

func getRecords(recordsIn *[]namecheap.DomainsDNSHostRecordDetailed) ([]namecheap.DomainsDNSHostRecord, error) {
	records := make([]namecheap.DomainsDNSHostRecord, len(*recordsIn))

	for i, rIn := range *recordsIn {
		if rIn.Name == nil {
			return nil, fmt.Errorf(
				"hostname missing for record",
			)
		}
		if rIn.Type == nil {
			return nil, fmt.Errorf(
				"type missing for record",
			)
		}

		record := namecheap.DomainsDNSHostRecord{
			HostName:   rIn.Name,
			RecordType: rIn.Type,
		}
		if rIn.Address != nil {
			record.Address = rIn.Address
		}
		if rIn.MXPref != nil {
			record.MXPref = namecheap.UInt8(uint8(*rIn.MXPref))
		}
		if rIn.TTL != nil {
			record.TTL = rIn.TTL
		}

		records[i] = record
	}

	return records, nil
}

// Adds a record to a domain
func addTestRecord(records []namecheap.DomainsDNSHostRecord, domain, key string) []namecheap.DomainsDNSHostRecord {
	return append(
		records,
		namecheap.DomainsDNSHostRecord{
			HostName:   &domain,
			RecordType: namecheap.String(namecheap.RecordTypeTXT),
			Address:    namecheap.String(key),
			TTL:        namecheap.Int(60),
		},
	)
}

// Removes a record from a domain
func removeTestRecord(recordsIn []namecheap.DomainsDNSHostRecord, domain string) []namecheap.DomainsDNSHostRecord {
	for i, rIn := range recordsIn {
		if *rIn.HostName == domain && *rIn.RecordType == namecheap.RecordTypeTXT {
			return append(recordsIn[:i], recordsIn[i+1:]...)
		}
	}

	return recordsIn
}
