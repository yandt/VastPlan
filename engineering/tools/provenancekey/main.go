// provenancekey generates a Verifier Provider signing key and public trust fragment.
package main

import (
	"encoding/base64"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"cdsoft.com.cn/VastPlan/core/shared/go/artifactprovenance"
	"cdsoft.com.cn/VastPlan/engineering/internal/signingkey"
)

func main() {
	providerID := flag.String("provider-id", "", "Verifier Provider ID")
	keyID := flag.String("key-id", "", "Verifier key ID")
	privateOut := flag.String("private-out", "", "PKCS#8 PEM 私钥输出")
	keyOut := flag.String("key-out", "", "公开 VerifierKey JSON 片段输出")
	flag.Parse()
	if *providerID == "" || *keyID == "" || *privateOut == "" || *keyOut == "" {
		flag.Usage()
		os.Exit(2)
	}
	publicKey, err := signingkey.Generate(*privateOut)
	if err != nil {
		fatal(err)
	}
	raw, err := json.MarshalIndent(artifactprovenance.VerifierKey{ProviderID: *providerID, KeyID: *keyID, PublicKey: base64.StdEncoding.EncodeToString(publicKey)}, "", "  ")
	if err != nil {
		fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(*keyOut), 0o755); err != nil {
		fatal(err)
	}
	file, err := os.OpenFile(*keyOut, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		fatal(err)
	}
	if _, err := file.Write(append(raw, '\n')); err != nil {
		_ = file.Close()
		fatal(err)
	}
	if err := file.Close(); err != nil {
		fatal(err)
	}
	fmt.Printf("已生成 Verifier Provider key %s/%s\n私钥: %s\n公开 key: %s\n", *providerID, *keyID, *privateOut, *keyOut)
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}
