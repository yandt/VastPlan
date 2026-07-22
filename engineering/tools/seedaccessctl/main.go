// Command seedaccessctl performs local-only Seed initialization and recovery
// operations. It intentionally has no network listener.
package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	seedaccess "cdsoft.com.cn/VastPlan/extensions/plugins/cn.vastplan.foundation.security.seed-access/seedaccess"
)

func main() {
	stateFile := flag.String("state-file", "", "Seed Access 状态绝对路径")
	operator := flag.String("operator", "", "首次初始化的 Seed Operator ID")
	passwordFile := flag.String("password-file", "", "owner-only 密码文件；初始化时必填")
	proofFile := flag.String("proof-file", "", "owner-only 本机恢复证明文件")
	ttl := flag.Duration("ttl", 10*time.Minute, "恢复租约有效期，最长 15 分钟")
	flag.Parse()
	if flag.NArg() != 1 || *stateFile == "" {
		fatal(errors.New("用法: seedaccessctl [flags] init|status|open-recovery|close-recovery"))
	}
	store := seedaccess.FileStore{Path: *stateFile}
	command := flag.Arg(0)
	var verifier seedaccess.LocalRecoveryVerifier
	if *proofFile != "" {
		verifier = seedaccess.FileRecoveryProofVerifier{Path: *proofFile}
	}
	authority, err := seedaccess.NewAuthority(store, verifier)
	if err != nil {
		fatal(err)
	}
	switch command {
	case "init":
		password, err := readOwnerOnly(*passwordFile)
		if err != nil {
			fatal(err)
		}
		defer clear(password)
		state, err := authority.Initialize(*operator, password)
		if err != nil {
			fatal(err)
		}
		printState(state)
	case "status":
		state, err := store.Load()
		if err != nil {
			fatal(err)
		}
		printState(state)
	case "open-recovery":
		proof, err := readOwnerOnly(*proofFile)
		if err != nil {
			fatal(err)
		}
		defer clear(proof)
		state, token, err := authority.OpenRecovery(mustState(store).Generation, proof, *ttl)
		if err != nil {
			fatal(err)
		}
		fmt.Printf("generation=%d phase=%s recovery_token=%s\n", state.Generation, state.Phase, token)
	case "close-recovery":
		state, err := authority.CloseRecovery(mustState(store).Generation)
		if err != nil {
			fatal(err)
		}
		printState(state)
	default:
		fatal(fmt.Errorf("未知命令 %q", command))
	}
}

func readOwnerOnly(path string) ([]byte, error) {
	if path == "" {
		return nil, errors.New("缺少 owner-only 输入文件")
	}
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm()&0o077 != 0 {
		return nil, errors.New("输入必须是 owner-only 普通文件")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return []byte(strings.TrimSuffix(string(raw), "\n")), nil
}

func mustState(store seedaccess.FileStore) seedaccess.State {
	state, err := store.Load()
	if err != nil {
		fatal(err)
	}
	return state
}

func printState(state seedaccess.State) {
	projection := struct {
		Version    int              `json:"version"`
		Generation uint64           `json:"generation"`
		Phase      seedaccess.Phase `json:"phase"`
		UpdatedAt  time.Time        `json:"updatedAt"`
	}{state.Version, state.Generation, state.Phase, state.UpdatedAt}
	raw, _ := json.Marshal(projection)
	fmt.Println(string(raw))
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "seedaccessctl:", err)
	os.Exit(1)
}
