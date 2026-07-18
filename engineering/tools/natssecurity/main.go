// natssecurity 生成 NKey 身份、最小 Subject ACL 和 mTLS NATS Server 配置。
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/nats-io/nkeys"

	"cdsoft.com.cn/VastPlan/core/shared/go/controlplane"
)

func main() {
	outDir := flag.String("out", "", "安全配置输出目录（不会覆盖已有文件）")
	listen := flag.String("listen", "0.0.0.0:4222", "NATS 监听地址")
	storeDir := flag.String("store-dir", "/var/lib/vastplan/nats", "JetStream 存储目录")
	tlsCert := flag.String("tls-cert", "", "NATS Server TLS 证书路径")
	tlsKey := flag.String("tls-key", "", "NATS Server TLS 私钥路径")
	tlsCA := flag.String("tls-ca", "", "签发服务端与客户端证书的 CA 路径")
	nodeCount := flag.Int("node-count", 1, "生成独立 node 身份数量")
	controllerCount := flag.Int("controller-count", 1, "生成独立 controller 身份数量")
	runtimeCount := flag.Int("runtime-count", 1, "生成独立 runtime 身份数量")
	flag.Parse()
	if *outDir == "" || *tlsCert == "" || *tlsKey == "" || *tlsCA == "" {
		flag.Usage()
		os.Exit(2)
	}
	if err := os.MkdirAll(*outDir, 0o700); err != nil {
		fatalf("创建输出目录失败: %v", err)
	}
	systemPublic, systemSeed := generateIdentity()
	if *nodeCount < 1 || *controllerCount < 1 || *runtimeCount < 1 {
		fatalf("node/controller/runtime 数量必须至少为 1")
	}
	identities := make([]controlplane.NKeyIdentity, 0, 1+*nodeCount+*controllerCount+*runtimeCount)
	seeds := map[string][]byte{"system.seed": systemSeed}
	addIdentity := func(role controlplane.SecurityRole, name, nodeID string) {
		publicKey, seed := generateIdentity()
		identities = append(identities, controlplane.NKeyIdentity{Name: name, Role: role, PublicKey: publicKey, NodeID: nodeID})
		seeds[name+".seed"] = seed
	}
	addIdentity(controlplane.RoleBootstrap, "bootstrap", "")
	for index := 1; index <= *controllerCount; index++ {
		addIdentity(controlplane.RoleController, fmt.Sprintf("controller-%d", index), "")
	}
	for index := 1; index <= *nodeCount; index++ {
		name := fmt.Sprintf("node-%d", index)
		addIdentity(controlplane.RoleNode, name, name)
	}
	for index := 1; index <= *runtimeCount; index++ {
		addIdentity(controlplane.RoleRuntime, fmt.Sprintf("runtime-%d", index), "")
	}
	config, err := controlplane.RenderNATSServerConfig(controlplane.ServerSecurityConfig{
		ServerName: "vastplan-controlplane", Listen: *listen, StoreDir: *storeDir,
		TLSCertFile: *tlsCert, TLSKeyFile: *tlsKey, TLSCAFile: *tlsCA,
		SystemPublicKey: systemPublic, Identities: identities,
	})
	if err != nil {
		fatalf("生成 NATS 配置失败: %v", err)
	}
	for filename, seed := range seeds {
		if err := writeExclusive(filepath.Join(*outDir, filename), append(seed, '\n'), 0o600); err != nil {
			fatalf("写入 %s 失败: %v", filename, err)
		}
	}
	if err := writeExclusive(filepath.Join(*outDir, "nats-server.conf"), []byte(config), 0o600); err != nil {
		fatalf("写入 NATS 配置失败: %v", err)
	}
	fmt.Printf("已生成 mTLS + NKey 配置: %s\n", *outDir)
	filenames := make([]string, 0, len(seeds))
	for filename := range seeds {
		filenames = append(filenames, filename)
	}
	sort.Strings(filenames)
	for _, filename := range filenames {
		fmt.Printf("  %s (0600)\n", filename)
	}
	fmt.Println("  nats-server.conf (0600，仅含公钥与 TLS 文件路径)")
}

func generateIdentity() (string, []byte) {
	pair, err := nkeys.CreateUser()
	if err != nil {
		fatalf("生成 NKey 用户失败: %v", err)
	}
	defer pair.Wipe()
	publicKey, err := pair.PublicKey()
	if err != nil {
		fatalf("读取 NKey 公钥失败: %v", err)
	}
	seed, err := pair.Seed()
	if err != nil {
		fatalf("读取 NKey seed 失败: %v", err)
	}
	return publicKey, append([]byte(nil), seed...)
}

func writeExclusive(filename string, content []byte, mode os.FileMode) error {
	file, err := os.OpenFile(filename, os.O_WRONLY|os.O_CREATE|os.O_EXCL, mode)
	if err != nil {
		return err
	}
	if _, err := file.Write(content); err != nil {
		_ = file.Close()
		return err
	}
	return file.Close()
}

func fatalf(format string, values ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", values...)
	os.Exit(1)
}
