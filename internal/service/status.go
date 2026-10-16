package service

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/cautem/quarshell-cli/internal/outfmt"
	"github.com/cautem/quarshell-sdk/go/cautem"
)

// StatusSnapshot is structured status for -o json|yaml.
type StatusSnapshot struct {
	Status               string `json:"status"`
	Authentication       string `json:"authentication"`
	Gateway              string `json:"gateway"`
	Version              string `json:"version,omitempty"`
	AllowUnauthenticated bool   `json:"allow_unauthenticated,omitempty"`
}

// Status prints gateway reachability and auth (OpenShell status).
func (a *App) Status() error {
	out := StatusSnapshot{
		Status:         "Disconnected",
		Authentication: "Unauthenticated",
	}
	u, err := a.currentGatewayURL()
	if err != nil {
		out.Gateway = ""
		return a.emitStatus(out)
	}
	out.Gateway = u
	ctx, cancel := a.withTimeout(TimeoutAPIShort)
	defer cancel()
	cli := cautem.NewWithToken(u, a.gatewayTokenForURL(u))
	if _, err := cli.Healthz(ctx); err == nil {
		out.Status = "Connected"
	}
	if info, err := cli.Info(ctx); err == nil {
		out.AllowUnauthenticated, _ = info["allow_unauthenticated"].(bool)
		if v, ok := info["version"].(string); ok {
			out.Version = v
		}
	}
	tok := strings.TrimSpace(a.gatewayTokenForURL(u))
	if tok == "" {
		if who, err := cli.Whoami(ctx); err == nil {
			switch auth, _ := who["auth"].(string); auth {
			case "authenticated":
				out.Authentication = "Authenticated"
			case "anonymous", "":
				out.Authentication = "Anonymous"
			default:
				out.Authentication = "Unauthenticated"
			}
		} else {
			out.Authentication = "Anonymous"
		}
	} else {
		if who, err := cli.Whoami(ctx); err == nil {
			switch auth, _ := who["auth"].(string); auth {
			case "authenticated":
				out.Authentication = "Authenticated"
			case "invalid_token":
				out.Authentication = "Unauthenticated"
			case "anonymous", "":
				if tok != "" {
					out.Authentication = "Authenticated"
				} else {
					out.Authentication = "Anonymous"
				}
			default:
				out.Authentication = "Authenticated"
			}
		} else {
			out.Authentication = "Authenticated"
		}
	}
	return a.emitStatus(out)
}

func (a *App) emitStatus(s StatusSnapshot) error {
	format := "text"
	if a != nil && a.OutputFormat != "" {
		format = a.OutputFormat
	}
	return outfmt.Emit(os.Stdout, format, func(w io.Writer) error {
		_, err := fmt.Fprintf(w, "Status: %s\nAuthentication: %s\nGateway: %s\n", s.Status, s.Authentication, s.Gateway)
		if err != nil {
			return err
		}
		if s.Version != "" {
			_, err = fmt.Fprintf(w, "Version: %s\n", s.Version)
		}
		if err == nil && s.AllowUnauthenticated {
			_, err = fmt.Fprintln(w, "WARNING: gateway allows unauthenticated local requests")
		}
		return err
	}, s)
}
