package databaseruntime

import (
	"crypto/tls"
	"crypto/x509"
	"errors"
)

func databaseTLSConfig(mode, serverName, host string) *tls.Config {
	if mode == "disable" {
		return nil
	}
	if mode == "verify-full" {
		if serverName == "" {
			serverName = host
		}
		return &tls.Config{MinVersion: tls.VersionTLS12, ServerName: serverName}
	}
	return &tls.Config{
		MinVersion: tls.VersionTLS12,
		// Hostname verification is intentionally replaced, not disabled: verify-ca
		// validates the complete certificate chain against system trust roots.
		InsecureSkipVerify: true, //nolint:gosec -- VerifyConnection performs chain verification below.
		VerifyConnection: func(state tls.ConnectionState) error {
			if len(state.PeerCertificates) == 0 {
				return errors.New("数据库 TLS 未提供服务端证书")
			}
			intermediates := x509.NewCertPool()
			for _, certificate := range state.PeerCertificates[1:] {
				intermediates.AddCert(certificate)
			}
			_, err := state.PeerCertificates[0].Verify(x509.VerifyOptions{Intermediates: intermediates, KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}})
			return err
		},
	}
}
