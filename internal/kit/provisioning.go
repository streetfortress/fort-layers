package kit

import (
	"path/filepath"
	"strings"

	"forge.streetfortress.cloud/oss/fort-layers/internal/tools"
)

// ProvisioningSecret reads one key of provisioning/secrets.yaml, the
// estate's operator-side secrets file (the core's verbs read the forge
// token and the pulumi passphrase from it; a layer reads the keys it
// documents). Decrypted through sops under the estate's rules, never
// printed.
func ProvisioningSecret(root, key string) (string, error) {
	cmd, err := tools.Command("sops", "--config", filepath.Join(root, ".sops.yaml"),
		"-d", "--extract", `["`+key+`"]`,
		filepath.Join(root, "provisioning/secrets.yaml"))
	if err != nil {
		return "", err
	}
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}
