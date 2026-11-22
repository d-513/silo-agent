package caddy

import (
	"slices"

	"silo.agent/internal/settingdef"
)

// DNSProvider is one DNS host Caddy can answer the ACME DNS challenge
// through: a caddy-dns module compiled into silo-caddy (images/caddy/
// Containerfile; TestDNSProvidersAreBuilt holds the two lists together).
// Its settings are caddy.dns.<ID>.<Key> in silo.yaml, and each Key is the
// JSON field of the libdns provider the module wraps, read from its source:
// they are passed to Caddy as they are.
type DNSProvider struct {
	// ID is the module's name (dns.providers.<ID>).
	ID   string
	Name string
	// Description says what credential to make, and how narrow it can be.
	Description string
	Settings    []settingdef.Def
}

func secret(key, label, desc string) settingdef.Def {
	return settingdef.Def{Key: key, Label: label, Description: desc, Secret: true}
}

func plain(key, label, desc string) settingdef.Def {
	return settingdef.Def{Key: key, Label: label, Description: desc}
}

var dnsProviders = []DNSProvider{
	{ID: "cloudflare", Name: "Cloudflare", Description: "An API token with Zone → DNS → Edit, limited to the zone Silo's names are in.", Settings: []settingdef.Def{
		secret("api_token", "API token", "Zone → DNS → Edit."),
		secret("zone_token", "Zone token", "Optional: a separate Zone → Zone → Read token, when the API token is limited to one zone and cannot list zones."),
	}},
	{ID: "route53", Name: "Amazon Route 53", Description: "An IAM user or role allowed route53:ChangeResourceRecordSets and route53:ListHostedZonesByName.", Settings: []settingdef.Def{
		plain("access_key_id", "Access key ID", ""),
		secret("secret_access_key", "Secret access key", ""),
		plain("region", "Region", "us-east-1 unless your account says otherwise."),
		plain("hosted_zone_id", "Hosted zone ID", "Optional: the zone, when Route 53 cannot find it by name."),
	}},
	{ID: "azure", Name: "Azure DNS", Description: "A service principal with DNS Zone Contributor on the zone.", Settings: []settingdef.Def{
		plain("tenant_id", "Tenant ID", ""),
		plain("client_id", "Client ID", ""),
		secret("client_secret", "Client secret", ""),
		plain("subscription_id", "Subscription ID", ""),
		plain("resource_group_name", "Resource group", "The resource group the DNS zone is in."),
	}},
	{ID: "bunny", Name: "Bunny DNS", Description: "Your account's API key.", Settings: []settingdef.Def{
		secret("access_key", "API key", ""),
	}},
	{ID: "desec", Name: "deSEC", Description: "A token from your deSEC account.", Settings: []settingdef.Def{
		secret("token", "Token", ""),
	}},
	{ID: "digitalocean", Name: "DigitalOcean", Description: "A personal access token with the domain scopes.", Settings: []settingdef.Def{
		secret("auth_token", "Access token", ""),
	}},
	{ID: "duckdns", Name: "Duck DNS", Description: "Your Duck DNS token. Duck DNS has no wildcards of its own: use a name under your duckdns.org one.", Settings: []settingdef.Def{
		secret("api_token", "Token", ""),
		plain("override_domain", "Override domain", "Optional: your duckdns.org name, when the challenge is for a name that points to it by CNAME."),
	}},
	{ID: "gandi", Name: "Gandi", Description: "A personal access token with the Manage domain name technical configurations permission.", Settings: []settingdef.Def{
		secret("bearer_token", "Personal access token", ""),
	}},
	{ID: "godaddy", Name: "GoDaddy", Description: "A production API key and secret, written key:secret.", Settings: []settingdef.Def{
		secret("api_token", "Key and secret", "key:secret"),
	}},
	{ID: "hetzner", Name: "Hetzner", Description: "A token of the Hetzner Console (Cloud) API with read and write on the project holding the zone.", Settings: []settingdef.Def{
		secret("api_token", "API token", ""),
	}},
	{ID: "ionos", Name: "IONOS", Description: "An API key, written prefix.secret.", Settings: []settingdef.Def{
		secret("auth_api_token", "API key", "prefix.secret"),
	}},
	{ID: "linode", Name: "Linode (Akamai)", Description: "A personal access token with Domains read/write.", Settings: []settingdef.Def{
		secret("api_token", "Access token", ""),
	}},
	{ID: "namecheap", Name: "Namecheap", Description: "API access must be on for the account, with this machine's public address allowed.", Settings: []settingdef.Def{
		plain("user", "User name", ""),
		secret("api_key", "API key", ""),
		plain("client_ip", "Public address", "Optional: the allowed address, when it cannot be found by itself."),
	}},
	{ID: "netcup", Name: "netcup", Description: "The customer number, an API key and the API password from the customer control panel.", Settings: []settingdef.Def{
		plain("customer_number", "Customer number", ""),
		secret("api_key", "API key", ""),
		secret("api_password", "API password", ""),
	}},
	{ID: "ovh", Name: "OVHcloud", Description: "An application key, its secret and a consumer key allowed GET, POST, PUT and DELETE on /domain/zone/*.", Settings: []settingdef.Def{
		plain("endpoint", "Endpoint", "ovh-eu, ovh-ca or ovh-us."),
		plain("application_key", "Application key", ""),
		secret("application_secret", "Application secret", ""),
		secret("consumer_key", "Consumer key", ""),
	}},
	{ID: "porkbun", Name: "Porkbun", Description: "An API key and secret, with API access turned on for the domain.", Settings: []settingdef.Def{
		secret("api_key", "API key", ""),
		secret("api_secret_key", "Secret key", ""),
	}},
	{ID: "vultr", Name: "Vultr", Description: "Your account's API key.", Settings: []settingdef.Def{
		secret("api_token", "API key", ""),
	}},
}

// DNSProviders lists every DNS provider silo-caddy has, by name.
func DNSProviders() []DNSProvider { return dnsProviders }

// DNSProviderOf is the provider with that id.
func DNSProviderOf(id string) (DNSProvider, bool) {
	i := slices.IndexFunc(dnsProviders, func(p DNSProvider) bool { return p.ID == id })
	if i < 0 {
		return DNSProvider{}, false
	}
	return dnsProviders[i], true
}
