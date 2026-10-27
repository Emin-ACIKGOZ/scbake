package templateregistry

import "strings"

// RegistryTokenEnvPrefix is the environment variable prefix for registry credentials.
//
//nolint:gosec // environment variable name prefix, not a credential
const RegistryTokenEnvPrefix = "SCBAKE_REGISTRY_TOKEN_"

// RegistryTokenEnvKey returns the environment variable name for a registry.
func RegistryTokenEnvKey(name string) string {
	return RegistryTokenEnvPrefix + strings.ToUpper(strings.ReplaceAll(name, "-", "_"))
}
