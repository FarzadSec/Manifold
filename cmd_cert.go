package main

import (
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/mhsanaei/3x-ui/v3/internal/config"
	"github.com/mhsanaei/3x-ui/v3/internal/database"
	"github.com/mhsanaei/3x-ui/v3/internal/web/service"
)

const certUsage = `Usage:
  manifold cert issue -domain NAME[,NAME...] [-email ADDR] [-port 80] [-staging]
  manifold cert issue -domain NAME[,NAME...] -dns cloudflare|arvancloud -dnsToken TOKEN [-email ADDR]
  manifold cert -webCert FILE -webCertKey FILE
  manifold cert -reset

issue gets a Let's Encrypt certificate and serves the panel with it; the panel renews it
on its own. NAME is a domain pointing at this server, or this server's public IP address,
which gets a six-day certificate. Over HTTP-01, port 80 (or -port) must be free and
reachable from the internet. -dns proves the names through the DNS provider instead, and
is the only way to a wildcard; the token may also come from MANIFOLD_DNS_TOKEN.
-webCert/-webCertKey serve the panel with files of your own; -reset stops serving a
certificate, so the panel answers over plain HTTP. Restart the panel afterwards.
`

// runCertCLI sets the panel's certificate from the command line, for when the panel's
// own pages cannot be reached.
func runCertCLI(args []string) int {
	if len(args) > 0 && args[0] == "issue" {
		return runCertIssue(args[1:])
	}
	fs := flag.NewFlagSet("cert", flag.ContinueOnError)
	certFile := fs.String("webCert", "", "certificate (full chain) file for the panel")
	keyFile := fs.String("webCertKey", "", "private key file for the panel")
	reset := fs.Bool("reset", false, "stop serving a certificate")
	if err := fs.Parse(args); err != nil || (!*reset && *certFile == "" && *keyFile == "") {
		fmt.Print(certUsage)
		return 2
	}
	if *reset {
		updateCert("", "")
	} else {
		updateCert(*certFile, *keyFile)
	}
	return 0
}

func runCertIssue(args []string) int {
	fs := flag.NewFlagSet("cert issue", flag.ContinueOnError)
	names := fs.String("domain", "", "domains or IP addresses, comma separated")
	email := fs.String("email", "", "address Let's Encrypt writes to about the certificate")
	port := fs.Int("port", 80, "port the HTTP-01 answer is served on")
	staging := fs.Bool("staging", false, "use Let's Encrypt's staging CA, for a trial run")
	dns := fs.String("dns", "", "prove the names with DNS-01 through cloudflare or arvancloud")
	dnsToken := fs.String("dnsToken", os.Getenv("MANIFOLD_DNS_TOKEN"), "the DNS provider's API token")
	if err := fs.Parse(args); err != nil || strings.TrimSpace(*names) == "" || fs.NArg() > 0 {
		fmt.Print(certUsage)
		return 2
	}
	in := service.CertificateInput{
		Domains: strings.Split(*names, ","), Challenge: "http-01", Port: *port,
		Email: *email, Staging: *staging, AutoRenew: true,
	}
	if *dns != "" {
		in.Challenge, in.Port, in.DNSProvider, in.DNSToken = "dns-01", 0, *dns, dnsToken
	}
	if err := database.InitDB(config.GetDBPath()); err != nil {
		fmt.Println("open database:", err)
		return 1
	}
	fmt.Println("Asking Let's Encrypt; this takes up to a few minutes...")
	view, err := (&service.CertificateService{}).IssueForPanel(in)
	if err != nil {
		fmt.Println("The certificate was not issued:", err)
		return 1
	}
	fmt.Printf("Issued for %s, valid until %s, renewed by the panel on its own.\n",
		strings.Join(view.Domains, ", "), time.UnixMilli(view.NotAfter).Format(time.DateTime))
	fmt.Println("cert:", view.CertFile)
	fmt.Println("key:", view.KeyFile)
	return 0
}
