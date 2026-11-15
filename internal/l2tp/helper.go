package l2tp

import (
	"bufio"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"
	"time"
)

const helperTimeout = 5 * time.Second

// ask sends one request to the panel's control socket and reads its answer.
func ask(socket string, req request) (reply, error) {
	ctx, cancel := context.WithTimeout(context.Background(), helperTimeout)
	defer cancel()
	conn, err := (&net.Dialer{}).DialContext(ctx, "unix", socket)
	if err != nil {
		return reply{}, err
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(helperTimeout))
	raw, err := json.Marshal(req)
	if err != nil {
		return reply{}, err
	}
	if _, err := conn.Write(append(raw, '\n')); err != nil {
		return reply{}, err
	}
	line, err := bufio.NewReader(conn).ReadBytes('\n')
	if err != nil {
		return reply{}, err
	}
	var out reply
	return out, json.Unmarshal(line, &out)
}

// readLogin parses what pppd's winbind plugin writes for one login: "Key:
// value" lines, "Key:: value" for base64, ended by a line holding a dot.
func readLogin(in io.Reader) (map[string]string, bool) {
	fields := map[string]string{}
	scanner := bufio.NewScanner(in)
	for scanner.Scan() {
		line := strings.TrimRight(scanner.Text(), "\r")
		if line == "." {
			return fields, true
		}
		key, value, found := strings.Cut(line, ":")
		if !found {
			continue
		}
		if encoded, isBase64 := strings.CutPrefix(value, ":"); isBase64 {
			decoded, err := base64.StdEncoding.DecodeString(strings.TrimSpace(encoded))
			if err != nil {
				continue
			}
			value = string(decoded)
		}
		fields[strings.ToLower(strings.TrimSpace(key))] = strings.TrimSpace(value)
	}
	return fields, false
}

// RunAuthHelper is the program pppd's winbind plugin starts for every login.
// It passes the MS-CHAPv2 response to the panel and answers in the plugin's
// own words. The exit status carries nothing; pppd reads the answer.
func RunAuthHelper(socket string, in io.Reader, out io.Writer) int {
	deny := func(reason string) int {
		fmt.Fprintf(out, "Authenticated: No\nAuthentication-Error: %s\n.\n", reason)
		return 0
	}
	fields, complete := readLogin(in)
	// A plain-text password means PAP, which the server never asks for.
	if _, plain := fields["password"]; !complete || plain || fields["username"] == "" {
		return deny(deniedLogin)
	}
	answer, err := ask(socket, request{
		Op:        "auth",
		User:      fields["username"],
		Challenge: fields["lanman-challenge"],
		Response:  fields["nt-response"],
	})
	if err != nil {
		return deny("The panel is not answering")
	}
	if !answer.OK {
		return deny(answer.Error)
	}
	fmt.Fprintf(out, "Authenticated: Yes\nUser-session-key: %s\n.\n", answer.Key)
	return 0
}

// RunHook is what pppd's ip-up and ip-down scripts run: it tells the panel
// that a session's link came up or went down. args are the interface, the
// peer's tunnel address and its public address; the rest is in pppd's environment.
func RunHook(event, socket string, args []string, getenv func(string) string) int {
	if len(args) < 3 || (event != "up" && event != "down") {
		return 2
	}
	pid, _ := strconv.Atoi(getenv("PPPD_PID"))
	req := request{Op: event, User: getenv("PEERNAME"), Interface: args[0], PID: pid, Address: args[1], Peer: args[2]}
	if event == "down" {
		req.Sent, _ = strconv.ParseInt(getenv("BYTES_SENT"), 10, 64)
		req.Received, _ = strconv.ParseInt(getenv("BYTES_RCVD"), 10, 64)
	}
	if _, err := ask(socket, req); err != nil {
		return 1
	}
	return 0
}
