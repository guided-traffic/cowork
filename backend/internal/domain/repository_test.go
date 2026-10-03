package domain

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// One identity for every way a remote is written (docs/adr/0066 D1): SSH,
// scp-style, HTTPS, a default and a non-default port, case, GitLab
// sub-groups.
func TestNormaliseRemote(t *testing.T) {
	for raw, want := range map[string]string{
		"git@github.com:guided-traffic/cowork.git":                      "github.com/guided-traffic/cowork",
		"https://github.com/guided-traffic/cowork":                      "github.com/guided-traffic/cowork",
		"ssh://git@github.com:22/guided-traffic/cowork/":                "github.com/guided-traffic/cowork",
		"https://github.com/guided-traffic/cowork.git/":                 "github.com/guided-traffic/cowork",
		"github.com:guided-traffic/cowork":                              "github.com/guided-traffic/cowork",
		"  git@github.com:guided-traffic/cowork.git\n":                  "github.com/guided-traffic/cowork",
		"HTTPS://user:secret@GitHub.COM/Guided-Traffic/Cowork.git":      "github.com/Guided-Traffic/Cowork",
		"GIT@GITHUB.COM:Org/Repo.git":                                   "github.com/Org/Repo",
		"https://ghp_token@github.com/org/repo":                         "github.com/org/repo",
		"ssh://git@gitlab.example.com:2222/group/sub/repo.git":          "gitlab.example.com:2222/group/sub/repo",
		"https://gitlab.com/group/subgroup/project":                     "gitlab.com/group/subgroup/project",
		"git@gitlab.com:group/subgroup/project.git":                     "gitlab.com/group/subgroup/project",
		"git://example.org:9418/repo.git":                               "example.org/repo",
		"http://example.org:80/x/y":                                     "example.org/x/y",
		"https://example.org:8443/x/y.git":                              "example.org:8443/x/y",
		"git+ssh://git@example.org/x//y":                                "example.org/x/y",
		"git@example.org:/srv/git/project.git":                          "example.org/srv/git/project",
		"ssh://[2001:db8::1]:2200/team/repo":                            "[2001:db8::1]:2200/team/repo",
		"https://codeberg.org/forgejo/forgejo.github.io.git":            "codeberg.org/forgejo/forgejo.github.io",
		"https://dev.azure.com/org/project/_git/repository":             "dev.azure.com/org/project/_git/repository",
		"ssh://git@ssh.dev.azure.com:22/v3/org/project/repository.git/": "ssh.dev.azure.com/v3/org/project/repository",
	} {
		got, err := NormaliseRemote(raw)
		require.NoError(t, err, raw)
		assert.Equal(t, want, got, raw)
	}
}

func TestNormaliseRemoteRefusesWhatNamesNoHost(t *testing.T) {
	for _, raw := range []string{
		"", "   ", "/srv/git/repo.git", "file:///srv/repo", "../repo", "repo", `C:\repos\x`, "C:/repos/x",
		"https://github.com", "https://github.com/", "https://github.com/.git", "https://host/a/../b",
		"https://host/a?b=c", "https://host/a#frag", "ftp://host/a/b", "ssh://:22/a", "https://host:99999/a",
		"git@host:a b/c", "https://ho_st/a",
	} {
		_, err := NormaliseRemote(raw)
		assert.ErrorIs(t, err, ErrNotARemote, "%q", raw)
	}
}

// A remote is stored and shown without a secret: an HTTP(S) URL loses its
// user information, another URL its password.
func TestSanitiseRemote(t *testing.T) {
	for raw, want := range map[string]string{
		"https://user:secret@github.com/org/repo.git": "https://github.com/org/repo.git",
		"https://ghp_token@github.com/org/repo":       "https://github.com/org/repo",
		"http://u@example.org/x":                      "http://example.org/x",
		"ssh://git:pw@example.org/x":                  "ssh://git@example.org/x",
		"ssh://git@example.org/x":                     "ssh://git@example.org/x",
		"git@github.com:org/repo.git":                 "git@github.com:org/repo.git",
		" https://github.com/org/repo ":               "https://github.com/org/repo",
	} {
		assert.Equal(t, want, SanitiseRemote(raw), raw)
	}
}

func TestNormaliseRepositoryPath(t *testing.T) {
	for in, want := range map[string]string{
		"":                   "",
		"/":                  "",
		".":                  "",
		"services/a":         "services/a",
		"/services//a/":      "services/a",
		`services\a`:         "services/a",
		"./services/./a":     "services/a",
		" services/a ":       "services/a",
		"Services/Großer-Ön": "Services/Großer-Ön",
	} {
		got, err := NormaliseRepositoryPath(in)
		require.NoError(t, err, in)
		assert.Equal(t, want, got, in)
	}
	for _, in := range []string{"../x", "a/../b", "a/b c"} {
		_, err := NormaliseRepositoryPath(in)
		assert.Error(t, err, in)
	}
}

func TestPathCovers(t *testing.T) {
	assert.True(t, PathCovers("", ""))
	assert.True(t, PathCovers("", "services/a"))
	assert.True(t, PathCovers("services/a", "services/a"))
	assert.True(t, PathCovers("services/a", "services/a/cmd"))
	assert.False(t, PathCovers("services/a", "services/ab"))
	assert.False(t, PathCovers("services/a", ""))
	assert.False(t, PathCovers("services/a", "services"))
}

func TestRepositoryNameAndOwner(t *testing.T) {
	assert.Equal(t, "cowork", RepositoryName("github.com/guided-traffic/cowork"))
	assert.Equal(t, "github.com/guided-traffic", RemoteOwner("github.com/guided-traffic/cowork"))
	assert.Equal(t, "gitlab.com/group/sub", RemoteOwner("gitlab.com/group/sub/project"))
}

// The proposed key (docs/adr/0066 D2): the initials of the parts, else the
// first letters; two to ten characters starting with a letter.
func TestProposeProjectKey(t *testing.T) {
	for name, want := range map[string]string{
		"valkey-operator":             "VO",
		"cowork":                      "COW",
		"go":                          "GO",
		"x":                           "XX",
		"my_cool.repo":                "MCR",
		"home-lab-manifests":          "HLM",
		"2fa-service":                 "FAS",
		"a-b-c-d-e-f-g-h-i-j-k-l":     "ABCDEFGHIJ",
		"日本":                          "REPO",
		"k8s":                         "K8S",
		"forgejo.github.io":           "FGI",
		"helm-charts-1":               "HC1",
		"Infrastructure":              "INF",
		"--weird--":                   "WEI",
		"guided-traffic.github.io-ui": "GTGIU",
	} {
		got := ProposeProjectKey(name)
		assert.Equal(t, want, got, name)
		assert.True(t, ValidProjectKey(got), "%s gave %s", name, got)
	}
}

func TestKeyCandidate(t *testing.T) {
	assert.Equal(t, "VO", KeyCandidate("VO", 1))
	assert.Equal(t, "VO2", KeyCandidate("VO", 2))
	assert.Equal(t, "ABCDEFGHI2", KeyCandidate("ABCDEFGHIJ", 2))
	assert.Equal(t, "ABCDEFGH10", KeyCandidate("ABCDEFGHIJ", 10))
	for n := 1; n < 100; n++ {
		assert.True(t, ValidProjectKey(KeyCandidate("ABCDEFGHIJ", n)))
	}
}
