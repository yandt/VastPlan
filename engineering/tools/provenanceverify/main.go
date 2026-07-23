// provenanceverify verifies a key-signed DSSE/SLSA statement and emits a
// signed Verification Record for repository and Node Agent admission.
package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"cdsoft.com.cn/VastPlan/core/kernels/backend/pluginservice"
	"cdsoft.com.cn/VastPlan/engineering/internal/provenanceprovider"
)

func main() {
	packageFile := flag.String("package", "", "最终插件 tar.gz")
	provenanceFile := flag.String("provenance", "", "DSSE/in-toto SLSA Provenance JSON")
	builderTrustFile := flag.String("builder-trust", "", "受信任 Builder Ed25519 公钥文档")
	policyFile := flag.String("policy", "", "静态 Verifier policy JSON")
	providerID := flag.String("provider-id", "", "Verifier Provider ID")
	providerKeyID := flag.String("provider-key-id", "", "Verifier 签名 key ID")
	providerKeyFile := flag.String("provider-sign-key", "", "Verifier Ed25519 PKCS#8 PEM 私钥")
	output := flag.String("output", "", "Verification Record 输出文件")
	flag.Parse()
	if *packageFile == "" || *provenanceFile == "" || *builderTrustFile == "" || *policyFile == "" || *providerID == "" || *providerKeyID == "" || *providerKeyFile == "" || *output == "" {
		flag.Usage()
		os.Exit(2)
	}
	packageBytes := mustRead(*packageFile)
	digest := sha256.Sum256(packageBytes)
	builderTrust, err := provenanceprovider.DecodeBuilderTrust(mustRead(*builderTrustFile))
	if err != nil {
		fatal(err)
	}
	policy, err := provenanceprovider.DecodePolicy(mustRead(*policyFile))
	if err != nil {
		fatal(err)
	}
	privateKey, err := pluginservice.LoadEd25519PrivateKeyPEM(*providerKeyFile)
	if err != nil {
		fatal(err)
	}
	record, err := provenanceprovider.Verify(provenanceprovider.Options{
		SubjectSHA256: hex.EncodeToString(digest[:]), Provenance: mustRead(*provenanceFile), BuilderTrust: builderTrust, Policy: policy,
		ProviderID: *providerID, ProviderKeyID: *providerKeyID, ProviderKey: privateKey, Now: time.Now().UTC(),
	})
	if err != nil {
		fatal(err)
	}
	raw, err := json.MarshalIndent(record, "", "  ")
	if err != nil {
		fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(*output), 0o755); err != nil {
		fatal(err)
	}
	if err := os.WriteFile(*output, append(raw, '\n'), 0o644); err != nil {
		fatal(err)
	}
	fmt.Printf("来源证明已验证: subject=%s provider=%s policy=%s\n", record.SubjectSHA256, record.ProviderID, record.PolicyID)
}

func mustRead(filename string) []byte {
	raw, err := os.ReadFile(filename)
	if err != nil {
		fatal(err)
	}
	return raw
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}
