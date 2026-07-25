package nodebootstrap

import "testing"

func TestPlanValidationRejectsUnsafeCredentialReferences(t *testing.T) {
	request := validRequest()
	plan := Plan{
		Target: request.Target, Release: request.Release, Node: request.Node,
		SSHIdentityCredential: "ssh.node-a", SSHKnownHostsCredential: "ssh.known-hosts",
	}
	for _, file := range request.SecretFiles {
		plan.SecretFiles = append(plan.SecretFiles, CredentialSecretFile{Credential: "node-a.material", Destination: file.Destination, Mode: file.Mode})
	}
	if err := plan.Validate(); err != nil {
		t.Fatalf("有效控制面计划被拒绝: %v", err)
	}
	plan.SSHIdentityCredential = "../../identity"
	if err := plan.Validate(); err == nil {
		t.Fatal("可歧义凭证引用必须被拒绝")
	}
	plan.SSHIdentityCredential = ".hidden"
	if err := plan.Validate(); err == nil {
		t.Fatal("控制面必须在保存前拒绝目录 Broker 无法解析的凭证名")
	}
	plan = Plan{Target: request.Target, Release: request.Release, Node: request.Node, SSHIdentityCredential: "ssh.node-a", SSHKnownHostsCredential: "ssh.known-hosts"}
	for _, file := range request.SecretFiles {
		plan.SecretFiles = append(plan.SecretFiles, CredentialSecretFile{Credential: "node-a.material", Destination: file.Destination, Mode: file.Mode})
	}
	plan.Node.NATSKey = SecretsRoot + "/custom.key"
	plan.SecretFiles[2].Destination = plan.Node.NATSKey
	if err := plan.Validate(); err == nil {
		t.Fatal("在线计划不得选择远端秘密文件名")
	}
}
