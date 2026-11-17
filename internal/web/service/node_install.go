package service

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/pem"
	"fmt"
	"net/netip"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
	"github.com/mhsanaei/3x-ui/v3/internal/logger"
	"github.com/mhsanaei/3x-ui/v3/internal/nodessh"
	"github.com/mhsanaei/3x-ui/v3/internal/util/common"
)

// nodeInstallScriptURL is the installer a node is set up with; tests point it elsewhere.
var nodeInstallScriptURL = "https://raw.githubusercontent.com/FarzadSec/Manifold/main/install.sh"

const (
	nodeInstallTimeout = 20 * time.Minute
	nodeInstallKeep    = time.Hour
	nodeTLSDir         = "/etc/manifold/node-tls"
	certMarker         = "==MANIFOLD-NODE-CERT=="
)

var tokenLine = regexp.MustCompile(`^apiToken:\s*(\S+)\s*$`)

// NodeInstallRequest installs (or updates) Manifold on a server over SSH and adds it as a
// node. SSH credentials are used for this job only and never stored.
type NodeInstallRequest struct {
	NodeId     int    `json:"nodeId" example:"0"`
	Name       string `json:"name" example:"de-fra-1"`
	Address    string `json:"address" example:"203.0.113.10"`
	PanelPort  int    `json:"panelPort" example:"2053"`
	SSHHost    string `json:"sshHost" example:""`
	SSHPort    int    `json:"sshPort" example:"22"`
	SSHUser    string `json:"sshUser" example:"root"`
	Password   string `json:"password" example:""`
	PrivateKey string `json:"privateKey" example:""`
	HostKey    string `json:"hostKey" example:"SHA256:Zm9vYmFy"`
	Update     bool   `json:"update" example:"false"`
}

// NodeInstallJob is what an install has done so far.
type NodeInstallJob struct {
	Id     string    `json:"id" example:"9f2c1a"`
	State  string    `json:"state" example:"running"`
	Log    []string  `json:"log"`
	NodeId int       `json:"nodeId" example:"3"`
	Error  string    `json:"error,omitempty" example:""`
	ended  time.Time `json:"-"`
}

var nodeInstalls = struct {
	sync.Mutex
	jobs map[string]*NodeInstallJob
}{jobs: map[string]*NodeInstallJob{}}

// InstallJob is a copy of the job's state, for polling.
func (s *NodeService) InstallJob(id string) (NodeInstallJob, bool) {
	nodeInstalls.Lock()
	defer nodeInstalls.Unlock()
	job, ok := nodeInstalls.jobs[id]
	if !ok {
		return NodeInstallJob{}, false
	}
	out := *job
	out.Log = append([]string{}, job.Log...)
	return out, true
}

func (job *NodeInstallJob) logf(format string, args ...any) {
	nodeInstalls.Lock()
	defer nodeInstalls.Unlock()
	job.Log = append(job.Log, fmt.Sprintf(format, args...))
}

func (job *NodeInstallJob) finish(nodeID int, err error) {
	nodeInstalls.Lock()
	defer nodeInstalls.Unlock()
	job.NodeId, job.State, job.ended = nodeID, "done", time.Now()
	if err != nil {
		job.State, job.Error = "failed", err.Error()
	}
}

func randomToken(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)[:n]
}

// StartInstall checks the request and runs the install in the background.
func (s *NodeService) StartInstall(req NodeInstallRequest) (string, error) {
	var existing *model.Node
	if req.NodeId > 0 {
		n, err := s.GetById(req.NodeId)
		if err != nil || n == nil {
			return "", common.NewError("node not found")
		}
		existing = n
		if req.Address == "" {
			req.Address = n.Address
		}
		if req.Name == "" {
			req.Name = n.Name
		}
		if req.PanelPort == 0 {
			req.PanelPort = n.Port
		}
	}
	if req.Update && existing == nil {
		return "", common.NewError("an update needs an existing node")
	}
	req.Name, req.Address = strings.TrimSpace(req.Name), strings.TrimSpace(req.Address)
	if req.Name == "" || req.Address == "" {
		return "", common.NewError("a node needs a name and an address")
	}
	if req.PanelPort <= 0 || req.PanelPort > 65535 {
		return "", common.NewError("node port must be 1-65535")
	}
	if req.SSHHost == "" {
		req.SSHHost = req.Address
	}
	if req.SSHUser == "" {
		req.SSHUser = "root"
	}
	if req.Password == "" && strings.TrimSpace(req.PrivateKey) == "" {
		return "", nodessh.ErrNoCredential
	}
	if strings.TrimSpace(req.HostKey) == "" {
		return "", common.NewError("read and accept the server's SSH host key first")
	}

	job := &NodeInstallJob{Id: randomToken(16), State: "running", Log: []string{}}
	nodeInstalls.Lock()
	for id, j := range nodeInstalls.jobs {
		if j.State != "running" && time.Since(j.ended) > nodeInstallKeep {
			delete(nodeInstalls.jobs, id)
		}
	}
	nodeInstalls.jobs[job.Id] = job
	nodeInstalls.Unlock()

	common.GoRecover("node-install", func() {
		nodeID, err := s.runInstall(job, req, existing)
		if err != nil {
			job.logf("✗ %v", err)
			logger.Warning("node install on", req.SSHHost, "failed:", err)
		}
		job.finish(nodeID, err)
	})
	return job.Id, nil
}

func (s *NodeService) runInstall(job *NodeInstallJob, req NodeInstallRequest, existing *model.Node) (int, error) {
	ctx, cancel := context.WithTimeout(context.Background(), nodeInstallTimeout)
	defer cancel()
	job.logf("→ SSH %s@%s:%d, host key %s", req.SSHUser, req.SSHHost, max(req.SSHPort, 22), req.HostKey)
	client, err := nodessh.Dial(ctx, nodessh.Target{
		Host: req.SSHHost, Port: req.SSHPort, User: req.SSHUser,
		Password: req.Password, PrivateKey: req.PrivateKey, HostKey: req.HostKey,
	})
	if err != nil {
		return 0, err
	}
	defer client.Close()

	basePath := "/" + randomToken(12) + "/"
	if existing != nil && existing.BasePath != "" && existing.BasePath != "/" {
		basePath = existing.BasePath
	}
	panelPass := randomToken(24)
	var token string
	var cert strings.Builder
	inCert := false
	err = nodessh.RunScript(ctx, client, nodeInstallScript(req, basePath, panelPass), func(line string) {
		switch {
		case inCert:
			cert.WriteString(line + "\n")
		case line == certMarker:
			inCert = true
		case tokenLine.MatchString(line):
			token = tokenLine.FindStringSubmatch(line)[1]
			job.logf("apiToken: (kept by the panel)")
		default:
			job.logf("%s", strings.ReplaceAll(line, panelPass, "••••••"))
		}
	})
	if err != nil {
		return 0, fmt.Errorf("the script on the server failed: %w", err)
	}
	if req.Update {
		job.logf("✓ Updated")
		return existing.Id, nil
	}
	pin, err := certPin(cert.String())
	if err != nil {
		return 0, err
	}
	if token == "" {
		return 0, common.NewError("the server printed no API token")
	}
	n := &model.Node{
		Name: req.Name, Address: req.Address, Port: req.PanelPort, Scheme: "https", BasePath: basePath,
		ApiToken: token, Enable: true, TlsVerifyMode: "pin", PinnedCertSha256: pin,
	}
	if existing != nil {
		kept := *existing
		kept.Address, kept.Port, kept.Scheme, kept.BasePath = n.Address, n.Port, n.Scheme, n.BasePath
		kept.ApiToken, kept.TlsVerifyMode, kept.PinnedCertSha256 = token, "pin", pin
		if err := s.Update(existing.Id, &kept); err != nil {
			return 0, err
		}
		job.logf("✓ Node %s now reaches the new install, its certificate pinned", existing.Name)
		return existing.Id, nil
	}
	if err := s.Create(n); err != nil {
		return 0, err
	}
	job.logf("✓ Added node %s, its certificate pinned", n.Name)
	return n.Id, nil
}

// certPin is the pin of the certificate the script printed, read over the pinned SSH channel.
func certPin(pemText string) (string, error) {
	block, _ := pem.Decode([]byte(pemText))
	if block == nil || block.Type != "CERTIFICATE" {
		return "", common.NewError("the server printed no certificate")
	}
	sum := sha256.Sum256(block.Bytes)
	return base64.StdEncoding.EncodeToString(sum[:]), nil
}

func subjectAltName(address string) string {
	if _, err := netip.ParseAddr(address); err == nil {
		return "IP:" + address
	}
	return "DNS:" + address
}

// nodeInstallScript installs or updates Manifold, gives a first install a self-signed
// certificate and a node-admin token, and prints both for the panel.
func nodeInstallScript(req NodeInstallRequest, basePath, panelPass string) string {
	q := nodessh.Quote
	var b strings.Builder
	b.WriteString("set -euo pipefail\n")
	b.WriteString(`SUDO=""; if [ "$(id -u)" -ne 0 ]; then SUDO="sudo -n"; fi` + "\n")
	b.WriteString(`echo "Server: $(uname -srm)"; if [ -r /etc/os-release ]; then . /etc/os-release; echo "OS: ${PRETTY_NAME:-unknown}"; fi` + "\n")
	b.WriteString("curl -fsSL " + q(nodeInstallScriptURL) + " -o /tmp/manifold-install.sh\n")
	if req.Update {
		b.WriteString("$SUDO bash /tmp/manifold-install.sh\n")
		return b.String()
	}
	port := strconv.Itoa(req.PanelPort)
	b.WriteString("$SUDO bash /tmp/manifold-install.sh --port " + q(port) + " --path " + q(basePath) +
		" --username " + q("admin-"+randomToken(6)) + " --password " + q(panelPass) + "\n")
	dir := nodeTLSDir
	b.WriteString("$SUDO mkdir -p " + q(dir) + "\n")
	b.WriteString("if ! $SUDO test -s " + q(dir+"/node.crt") + "; then\n")
	b.WriteString("  $SUDO openssl req -x509 -newkey ec -pkeyopt ec_paramgen_curve:prime256v1 -nodes -days 3650 -subj /CN=manifold-node -addext " +
		q("subjectAltName="+subjectAltName(req.Address)) + " -keyout " + q(dir+"/node.key") + " -out " + q(dir+"/node.crt") + " 2>/dev/null\n")
	b.WriteString("fi\n")
	bin := "/usr/local/manifold/manifold"
	b.WriteString("$SUDO " + bin + " cert -webCert " + q(dir+"/node.crt") + " -webCertKey " + q(dir+"/node.key") + "\n")
	b.WriteString("$SUDO " + bin + " setting -port " + q(port) + " -webBasePath " + q(basePath) + "\n")
	b.WriteString("$SUDO systemctl restart manifold\n")
	b.WriteString("$SUDO " + bin + " setting -getApiToken -tokenName manifold-master -tokenScope node-admin | grep '^apiToken:'\n")
	b.WriteString("echo " + q(certMarker) + "\n")
	b.WriteString("$SUDO cat " + q(dir+"/node.crt") + "\n")
	return b.String()
}
