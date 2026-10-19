package host

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"time"
)

func directCertificate() (tls.Certificate, error) {
	path, err := configPath()
	if err != nil {
		return tls.Certificate{}, err
	}
	dir := path + ".tls"
	certPath, keyPath := filepath.Join(dir, "certificate.pem"), filepath.Join(dir, "key.pem")
	certificate, err := os.ReadFile(certPath)
	if os.IsNotExist(err) {
		key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		if err != nil {
			return tls.Certificate{}, err
		}
		serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
		if err != nil {
			return tls.Certificate{}, err
		}
		template := &x509.Certificate{
			SerialNumber: serial,
			Subject: pkix.Name{CommonName: "Lichess Engine Host"},
			NotBefore: time.Now().Add(-time.Hour),
			NotAfter: time.Now().AddDate(5, 0, 0),
			DNSNames: []string{"localhost"},
			IPAddresses: []net.IP{net.ParseIP("127.0.0.1"), net.ParseIP("::1")},
			KeyUsage: x509.KeyUsageDigitalSignature,
			ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
			BasicConstraintsValid: true,
		}
		der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
		if err != nil {
			return tls.Certificate{}, err
		}
		privateKey, err := x509.MarshalPKCS8PrivateKey(key)
		if err != nil {
			return tls.Certificate{}, err
		}
		if err := os.MkdirAll(dir, 0700); err != nil {
			return tls.Certificate{}, err
		}
		if err := os.WriteFile(keyPath, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: privateKey}), 0600); err != nil {
			return tls.Certificate{}, err
		}
		certificate = pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
		if err := os.WriteFile(certPath, certificate, 0600); err != nil {
			return tls.Certificate{}, err
		}
	} else if err != nil {
		return tls.Certificate{}, err
	}
	key, err := os.ReadFile(keyPath)
	if err != nil {
		return tls.Certificate{}, err
	}
	return tls.X509KeyPair(certificate, key)
}