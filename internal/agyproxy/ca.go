package agyproxy

// CUSTOM (cpamc-custom)

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"time"
)

type authority struct {
	certificate *x509.Certificate
	key         *ecdsa.PrivateKey
	public      []byte
}

func loadAuthority(dir string) (*authority, error) {
	if errMkdir := os.MkdirAll(dir, 0700); errMkdir != nil {
		return nil, errMkdir
	}
	path := filepath.Join(dir, "ca.pem")
	data, errRead := os.ReadFile(path)
	if errRead != nil && !os.IsNotExist(errRead) {
		return nil, errRead
	}
	if os.IsNotExist(errRead) {
		curve := elliptic.P256()
		key, errKey := ecdsa.GenerateKey(curve, rand.Reader)
		if errKey != nil {
			return nil, errKey
		}
		now := time.Now()
		unixNano := now.UnixNano()
		serial := big.NewInt(unixNano)
		subject := pkix.Name{CommonName: "CPAMC agy proxy CA"}
		template := &x509.Certificate{SerialNumber: serial, Subject: subject, NotBefore: now.Add(-time.Hour), NotAfter: now.AddDate(10, 0, 0), IsCA: true, BasicConstraintsValid: true, MaxPathLenZero: true, KeyUsage: x509.KeyUsageCertSign | x509.KeyUsageCRLSign}
		der, errCreate := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
		if errCreate != nil {
			return nil, errCreate
		}
		keyDER, errMarshal := x509.MarshalECPrivateKey(key)
		if errMarshal != nil {
			return nil, errMarshal
		}
		certBlock := &pem.Block{Type: "CERTIFICATE", Bytes: der}
		keyBlock := &pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER}
		data = pem.EncodeToMemory(certBlock)
		keyPEM := pem.EncodeToMemory(keyBlock)
		data = append(data, keyPEM...)
		if errWrite := writePrivate(path, data); errWrite != nil {
			return nil, errWrite
		}
	}
	certBlock, rest := pem.Decode(data)
	keyBlock, _ := pem.Decode(rest)
	if certBlock == nil || keyBlock == nil {
		return nil, fmt.Errorf("invalid agy CA file")
	}
	cert, errCert := x509.ParseCertificate(certBlock.Bytes)
	if errCert != nil {
		return nil, errCert
	}
	key, errKey := x509.ParseECPrivateKey(keyBlock.Bytes)
	if errKey != nil {
		return nil, errKey
	}
	publicKey, ok := cert.PublicKey.(*ecdsa.PublicKey)
	now := time.Now()
	if !ok || !cert.IsCA || !publicKey.Equal(&key.PublicKey) || now.After(cert.NotAfter) || now.Before(cert.NotBefore) {
		return nil, fmt.Errorf("invalid or expired agy CA")
	}
	public := pem.EncodeToMemory(certBlock)
	return &authority{certificate: cert, key: key, public: public}, nil
}

func (a *authority) leaf(host string) (*tls.Certificate, error) {
	curve := elliptic.P256()
	key, errKey := ecdsa.GenerateKey(curve, rand.Reader)
	if errKey != nil {
		return nil, errKey
	}
	now := time.Now()
	unixNano := now.UnixNano()
	serial := big.NewInt(unixNano)
	subject := pkix.Name{CommonName: host}
	template := &x509.Certificate{SerialNumber: serial, Subject: subject, DNSNames: []string{host}, NotBefore: now.Add(-time.Hour), NotAfter: now.AddDate(0, 0, 7), KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}
	if ip := net.ParseIP(host); ip != nil {
		template.DNSNames = nil
		template.IPAddresses = []net.IP{ip}
	}
	der, errCreate := x509.CreateCertificate(rand.Reader, template, a.certificate, &key.PublicKey, a.key)
	if errCreate != nil {
		return nil, errCreate
	}
	return &tls.Certificate{Certificate: [][]byte{der, a.certificate.Raw}, PrivateKey: key}, nil
}

func writePrivate(path string, data []byte) error {
	dir := filepath.Dir(path)
	file, errCreate := os.CreateTemp(dir, ".agy-*")
	if errCreate != nil {
		return errCreate
	}
	name := file.Name()
	defer os.Remove(name)
	_, errWrite := file.Write(data)
	errClose := file.Close()
	if errWrite != nil {
		return errWrite
	}
	if errClose != nil {
		return errClose
	}
	return os.Rename(name, path)
}
