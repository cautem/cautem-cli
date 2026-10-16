package service

import "time"

// Named CLI deadlines. Always derive from the command context (see App.withTimeout)
// so SIGINT/SIGTERM cancels in-flight work.
const (
	// TimeoutProbe — healthz, dial, ensure polls.
	TimeoutProbe = 2 * time.Second
	// TimeoutProbeFast — tight loops while waiting for a local listener.
	TimeoutProbeFast = 500 * time.Millisecond
	// TimeoutDial — single TCP dial attempt.
	TimeoutDial = 300 * time.Millisecond

	// TimeoutAPI — gateway / Engine CRUD and short RPCs.
	TimeoutAPI = 15 * time.Second
	// TimeoutAPIShort — status snippets, whoami, info.
	TimeoutAPIShort = 5 * time.Second
	// TimeoutAPILong — policy apply, provider put, inference set.
	TimeoutAPILong = 30 * time.Second

	// TimeoutWait — policy wait, browser login, landlock probe.
	TimeoutWait = 2 * time.Minute
	// TimeoutExecGrace lets Gateway deliver OpenShell's terminal exit event after
	// the server-side execution deadline has elapsed.
	TimeoutExecGrace = 5 * time.Second
	// TimeoutWaitLong — OIDC / interactive login ceiling.
	TimeoutWaitLong = 4 * time.Minute

	// TimeoutWork — sandbox create/pull/copy/start.
	TimeoutWork = 10 * time.Minute
	// TimeoutWorkShort — stop/start container, agent-config stage.
	TimeoutWorkShort = 5 * time.Minute
	// TimeoutCopy — docker cp style transfers.
	TimeoutCopy = 5 * time.Minute

	// TimeoutPolicyWait — cautem policy set --wait default cap.
	TimeoutPolicyWait = 60 * time.Second

	// TimeoutEmit — best-effort proc/log post (never block UX).
	TimeoutEmit = 2 * time.Second
)

// Startup, refresh, and interactive login defaults.
const (
	forwardStartupDelay        = 300 * time.Millisecond
	gatewayStartupTimeout      = 8 * time.Second
	gatewayStartupPollInterval = 200 * time.Millisecond
	credentialRefreshInterval  = 30 * time.Second
	loginHeaderReadTimeout     = 5 * time.Second
	loginTimeout               = 3 * time.Minute
)
