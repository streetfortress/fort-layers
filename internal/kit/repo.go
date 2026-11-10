package kit

import (
	"fmt"
	"regexp"
	"strings"
)

// Repo URLs, the core's spellings (sfi/forts internal/estate): an estate
// is named by its git URL in estate.toml, in a hub table, in a serves
// entry and in a link, and two spellings of one repo must read as one.

var scpLikeRE = regexp.MustCompile(`^([^@]+@)?([^:/]+):([^/].*)$`)

// ParseRepo splits a repo URL into forge host, owner and name:
// ssh://git@host:port/owner/name(.git), https://host/owner/name,
// git@host:owner/name.
func ParseRepo(repo string) (host, owner, name string, err error) {
	s := strings.TrimSuffix(strings.TrimRight(repo, "/"), ".git")
	var path string
	switch {
	case strings.Contains(s, "://"):
		rest := s[strings.Index(s, "://")+3:]
		slash := strings.Index(rest, "/")
		if slash < 0 {
			return "", "", "", fmt.Errorf("%q: no path", repo)
		}
		host, path = rest[:slash], rest[slash+1:]
		if at := strings.LastIndex(host, "@"); at >= 0 {
			host = host[at+1:]
		}
		if colon := strings.Index(host, ":"); colon >= 0 {
			host = host[:colon]
		}
	default:
		m := scpLikeRE.FindStringSubmatch(s)
		if m == nil {
			return "", "", "", fmt.Errorf("%q: not a repo URL", repo)
		}
		host, path = m[2], m[3]
	}
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) < 2 || parts[len(parts)-1] == "" {
		return "", "", "", fmt.Errorf("%q: want <owner>/<name>", repo)
	}
	return host, parts[len(parts)-2], parts[len(parts)-1], nil
}

// Ref is an estate URL reduced to <owner>/<name> (sfi/staging): how an
// estate is named in a finding. "" for a URL that does not parse.
func Ref(repo string) string {
	_, owner, name, err := ParseRepo(repo)
	if err != nil {
		return ""
	}
	return owner + "/" + name
}

// ShortName is the last label of an estate URL (staging): what a
// subscription file and a link are named after, the core's convention.
func ShortName(repo string) string {
	_, _, name, err := ParseRepo(repo)
	if err != nil {
		return ""
	}
	return name
}

// WithGit is a repo URL with its .git suffix, the form a flux
// GitRepository carries.
func WithGit(repo string) string {
	if strings.HasSuffix(repo, ".git") {
		return repo
	}
	return repo + ".git"
}

// SameRepo says whether two URLs name one repo: the same forge host,
// owner and name, whatever the scheme, the port and the .git suffix.
func SameRepo(a, b string) bool {
	ah, ao, an, err := ParseRepo(a)
	if err != nil {
		return false
	}
	bh, bo, bn, err := ParseRepo(b)
	if err != nil {
		return false
	}
	return ah == bh && ao == bo && an == bn
}
