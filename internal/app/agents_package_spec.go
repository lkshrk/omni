package app

import (
	"regexp"
	"strings"
)

type agentPackageSpec struct {
	source      string
	ref         string
	credentials bool
	display     string
}

var (
	agentSpecScheme = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9+.-]*$`)
	// A ref shaped like host/path means the @ belonged to userinfo, not to a ref.
	agentSpecHostRef = regexp.MustCompile(`^[^/]*\.[^/]*/`)
)

// parseAgentPackageSpec treats text before an @ as credentials unless it is ssh's git user or an owner/repo.
func parseAgentPackageSpec(spec string, withRef bool) agentPackageSpec {
	out := agentPackageSpec{source: spec}
	scheme, rest, hasScheme := strings.Cut(spec, "://")
	if hasScheme && agentSpecScheme.MatchString(scheme) {
		authority, path, hasPath := strings.Cut(rest, "/")
		if at := strings.LastIndex(path, "@"); at >= 0 {
			if !withRef {
				out.credentials = true
			} else {
				out.ref, path = path[at+1:], path[:at]
				out.source = strings.TrimSuffix(spec, "@"+out.ref)
			}
		}
		host := authority
		if at := strings.LastIndex(authority, "@"); at >= 0 {
			host = authority[at+1:]
			out.credentials = out.credentials || authority[:at] != "git"
		}
		bare := host
		if strings.HasPrefix(bare, "[") {
			if end := strings.Index(bare, "]"); end >= 0 {
				bare = "ipv6" + bare[end+1:]
			}
		}
		if hostname, port, ok := strings.Cut(bare, ":"); ok && (hostname == "" || strings.Trim(port, "0123456789") != "") {
			out.credentials = true
		}
		out.display = scheme + "://" + host
		if hasPath {
			out.display += "/" + path
		}
		if out.credentials {
			out.display = scheme + "://…"
		}
	} else {
		out.display = spec
		if at := strings.Index(spec, "@"); at >= 0 {
			prefix, rest := spec[:at], spec[at+1:]
			switch {
			case prefix == "git":
				if at := strings.LastIndex(rest, "@"); withRef && at >= 0 {
					out.ref, rest = rest[at+1:], rest[:at]
					out.source = "git@" + rest
				}
				out.display = "git@" + rest
			case strings.Contains(prefix, "/") && !strings.Contains(prefix, ":") && withRef:
				out.source, out.ref, out.display = prefix, rest, prefix
			default:
				out.credentials = true
			}
		}
		if out.credentials {
			out.display = "…"
		}
	}
	if agentSpecHostRef.MatchString(out.ref) || strings.ContainsAny(out.source, "?#") || strings.ContainsAny(out.ref, "?#") {
		out.credentials = true
		if hasScheme && agentSpecScheme.MatchString(scheme) {
			out.display = scheme + "://…"
		} else {
			out.display = "…"
		}
	}
	return out
}
