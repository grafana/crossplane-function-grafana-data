// Package clients provides a way to create clients from the Grafana Crossplane providerConfig using the Grafana Terraform provider
package clients

import (
	"encoding/json"

	onCallAPI "github.com/grafana/amixr-api-go-client"
	"github.com/grafana/crossplane-provider-grafana/apis/v1beta1"
	"github.com/grafana/grafana-app-sdk/k8s"
	assertsapi "github.com/grafana/grafana-asserts-public-clients/go/gcom"
	"github.com/grafana/grafana-com-public-clients/go/gcom"
	goapi "github.com/grafana/grafana-openapi-client-go/client"
	"github.com/grafana/k6-cloud-openapi-client-go/k6"
	"github.com/grafana/machine-learning-go-client/mlapi"
	"github.com/grafana/slo-openapi-client/go/slo"
	SMAPI "github.com/grafana/synthetic-monitoring-api-go-client"
	grafanaProvider "github.com/grafana/terraform-provider-grafana/v4/pkg/provider"
	"github.com/hashicorp/terraform-plugin-framework/types"
	v1 "k8s.io/api/core/v1"
)

// Client struct with all known clients (~copy/paste from TF provider)
type Client struct {
	GrafanaAPI            *goapi.GrafanaHTTPAPI
	GrafanaAppPlatformAPI *k8s.ClientRegistry
	GrafanaCloudAPI       *gcom.APIClient
	SMAPI                 *SMAPI.Client
	MLAPI                 *mlapi.Client
	OnCallClient          *onCallAPI.Client
	SLOClient             *slo.APIClient
	AssertsAPIClient      *assertsapi.APIClient
	K6APIClient           *k6.APIClient
	// in internal package
	// CloudProviderAPI      *cloudproviderapi.Client
	// ConnectionsAPIClient  *connectionsapi.Client
	// FleetManagementClient *fleetmanagementapi.Client
	// FrontendO11yAPIClient *frontendo11yapi.Client
	// K6APIConfig *k6providerapi.K6APIConfig
}

// NewClientsFromProviderConfig creates a Client struct from a Crossplane ProviderConfig/secret
func NewClientsFromProviderConfig(pc *v1beta1.ProviderConfig, secret *v1.Secret, secretKey string) (*Client, error) {
	var credentials map[string]string
	err := json.Unmarshal(secret.Data[secretKey], &credentials)
	if err != nil {
		return nil, err
	}

	crcfg := createCrossplaneConfiguration(pc, credentials)

	cfg, err := createTFConfiguration(crcfg)
	if err != nil {
		return nil, err
	}

	clients, err := grafanaProvider.CreateClients(*cfg)
	if err != nil {
		return nil, err
	}

	client := Client{
		GrafanaAPI:            clients.GrafanaAPI,
		GrafanaAppPlatformAPI: clients.GrafanaAppPlatformAPI,
		GrafanaCloudAPI:       clients.GrafanaCloudAPI,
		SMAPI:                 clients.SMAPI,
		MLAPI:                 clients.MLAPI,
		OnCallClient:          clients.OnCallClient,
		SLOClient:             clients.SLOClient,
		AssertsAPIClient:      clients.AssertsAPIClient,
		K6APIClient:           clients.K6APIClient,
	}

	return &client, nil
}

// createCrossplaneConfiguration from the Crossplane ProviderConfig
//
//nolint:gocyclo // ignore
func createCrossplaneConfiguration(pc *v1beta1.ProviderConfig, creds map[string]string) map[string]any {
	// Set credentials in Terraform provider configuration.
	// https://registry.terraform.io/providers/grafana/grafana/latest/docs
	config := map[string]any{}
	for _, k := range []string{
		"auth",
		"url",

		"cloud_access_policy_token",
		"cloud_api_url",

		"cloud_provider_access_token",
		"cloud_provider_url",

		"connections_api_access_token",
		"connections_api_url",

		"fleet_management_auth",
		"fleet_management_url",

		"frontend_o11y_api_access_token",

		"oncall_access_token",
		"oncall_url",

		"sm_access_token",
		"sm_url",

		"cloud_api_key", // don't see it in the TF config
		"org_id",        // don't see it in the TF config

		// required for k6 resources
		"stack_id",
		"k6_access_token",
	} {
		if v, ok := creds[k]; ok {
			config[k] = v
		}
	}

	if pc.Spec.URL != "" {
		config["url"] = pc.Spec.URL
	}
	if pc.Spec.CloudAPIURL != "" {
		config["cloud_api_url"] = pc.Spec.CloudAPIURL
	}
	if pc.Spec.CloudProviderURL != "" {
		config["cloud_provider_url"] = pc.Spec.CloudProviderURL
	}
	if pc.Spec.ConnectionsAPIURL != "" {
		config["connections_api_url"] = pc.Spec.ConnectionsAPIURL
	}
	if pc.Spec.FleetManagementURL != "" {
		config["fleet_management_url"] = pc.Spec.FleetManagementURL
	}
	if pc.Spec.OnCallURL != "" {
		config["oncall_url"] = pc.Spec.OnCallURL
	}
	if pc.Spec.SMURL != "" {
		config["sm_url"] = pc.Spec.SMURL
	}
	if pc.Spec.OrgID != nil {
		config["org_id"] = *pc.Spec.OrgID
	}
	if pc.Spec.StackID != nil {
		config["stack_id"] = *pc.Spec.StackID
	}
	return config
}

// mostly copied from terraform-provider-grafana/pkg/provider/legacy_provider.go#configure()
// commented out some bits that need additional logic, not required for the POC
func createTFConfiguration(d map[string]any) (*grafanaProvider.ProviderConfig, error) {
	cfg := grafanaProvider.ProviderConfig{
		Auth:                       stringValueOrNull(d, "auth"),
		URL:                        stringValueOrNull(d, "url"),
		OrgID:                      int64ValueOrNull(d, "org_id"),
		StackID:                    int64ValueOrNull(d, "stack_id"),
		TLSKey:                     stringValueOrNull(d, "tls_key"),
		TLSCert:                    stringValueOrNull(d, "tls_cert"),
		CACert:                     stringValueOrNull(d, "ca_cert"),
		InsecureSkipVerify:         boolValueOrNull(d, "insecure_skip_verify"),
		CloudAccessPolicyToken:     stringValueOrNull(d, "cloud_access_policy_token"),
		CloudAPIURL:                stringValueOrNull(d, "cloud_api_url"),
		SMAccessToken:              stringValueOrNull(d, "sm_access_token"),
		SMURL:                      stringValueOrNull(d, "sm_url"),
		OncallAccessToken:          stringValueOrNull(d, "oncall_access_token"),
		OncallURL:                  stringValueOrNull(d, "oncall_url"),
		CloudProviderAccessToken:   stringValueOrNull(d, "cloud_provider_access_token"),
		CloudProviderURL:           stringValueOrNull(d, "cloud_provider_url"),
		ConnectionsAPIAccessToken:  stringValueOrNull(d, "connections_api_access_token"),
		ConnectionsAPIURL:          stringValueOrNull(d, "connections_api_url"),
		FleetManagementAuth:        stringValueOrNull(d, "fleet_management_auth"),
		FleetManagementURL:         stringValueOrNull(d, "fleet_management_url"),
		FrontendO11yAPIAccessToken: stringValueOrNull(d, "frontend_o11y_api_access_token"),
		K6URL:                      stringValueOrNull(d, "k6_url"),
		K6AccessToken:              stringValueOrNull(d, "k6_access_token"),
		StoreDashboardSha256:       boolValueOrNull(d, "store_dashboard_sha256"),
		//HTTPHeaders:                headers,
		//Retries: int64ValueOrNull(d, "retries"),
		//RetryStatusCodes:           statusCodes,
		//RetryWait: types.Int64Value(int64(d.Get("retry_wait").(int))),
		//UserAgent:                  types.StringValue(p.UserAgent("terraform-provider-grafana", version)),
		//Version:                    types.StringValue(version),
	}
	if err := cfg.SetDefaults(); err != nil {
		return nil, err
	}
	return &cfg, nil
}

func stringValueOrNull(d map[string]any, key string) types.String {
	if v, ok := d[key]; ok {
		return types.StringValue(v.(string))
	}
	return types.StringNull()
}

func boolValueOrNull(d map[string]any, key string) types.Bool {
	if v, ok := d[key]; ok {
		return types.BoolValue(v.(bool))
	}
	return types.BoolNull()
}

func int64ValueOrNull(d map[string]any, key string) types.Int64 {
	if v, ok := d[key]; ok {
		return types.Int64Value(int64(v.(int)))
	}
	return types.Int64Null()
}
