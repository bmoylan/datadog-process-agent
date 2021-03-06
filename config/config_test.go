package config

import (
	"fmt"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/DataDog/gopsutil/process"
	"github.com/go-ini/ini"
	"github.com/stretchr/testify/assert"
	"gopkg.in/yaml.v2"
)

func TestBlacklist(t *testing.T) {
	testBlacklist := []string{
		"^getty",
		"^acpid",
		"^atd",
		"^upstart-udev-bridge",
		"^upstart-socket-bridge",
		"^upstart-file-bridge",
		"^dhclient",
		"^dhclient3",
		"^rpc",
		"^dbus-daemon",
		"udevd",
		"^/sbin/",
		"^/usr/sbin/",
		"^/var/ossec/bin/ossec",
		"^rsyslogd",
		"^whoopsie$",
		"^cron$",
		"^CRON$",
		"^/usr/lib/postfix/master$",
		"^qmgr",
		"^pickup",
		"^sleep",
		"^/lib/systemd/systemd-logind$",
		"^/usr/local/bin/goshe dnsmasq$",
	}
	blacklist := make([]*regexp.Regexp, 0, len(testBlacklist))
	for _, b := range testBlacklist {
		r, err := regexp.Compile(b)
		if err == nil {
			blacklist = append(blacklist, r)
		}
	}
	cases := []struct {
		cmdline     []string
		blacklisted bool
	}{
		{[]string{"getty", "-foo", "-bar"}, true},
		{[]string{"rpcbind", "-x"}, true},
		{[]string{"my-rpc-app", "-config foo.ini"}, false},
		{[]string{"rpc.statd", "-L"}, true},
		{[]string{"/usr/sbin/irqbalance"}, true},
	}

	for _, c := range cases {
		assert.Equal(t, c.blacklisted, IsBlacklisted(c.cmdline, blacklist),
			fmt.Sprintf("Case %v failed", c))
	}
}

func TestOnlyEnvConfig(t *testing.T) {
	// setting an API Key should be enough to generate valid config
	os.Setenv("DD_API_KEY", "apikey_from_env")

	agentConfig, _ := NewAgentConfig(nil, nil)
	assert.Equal(t, "apikey_from_env", agentConfig.APIKey)

	os.Setenv("DD_API_KEY", "")
}

func TestOnlyEnvConfigArgsScrubbingEnabled(t *testing.T) {
	os.Setenv("DD_CUSTOM_SENSITIVE_WORDS", "*password*,consul_token,*api_key")

	agentConfig, _ := NewAgentConfig(nil, nil)
	assert.Equal(t, true, agentConfig.Scrubber.Enabled)

	cases := []struct {
		cmdline       []string
		parsedCmdline []string
	}{
		{
			[]string{"spidly", "--mypasswords=123,456", "consul_token", "1234", "--dd_api_key=1234"},
			[]string{"spidly", "--mypasswords=********", "consul_token", "********", "--dd_api_key=********"},
		},
	}

	for i := range cases {
		cases[i].cmdline, _ = agentConfig.Scrubber.scrubCommand(cases[i].cmdline)
		assert.Equal(t, cases[i].parsedCmdline, cases[i].cmdline)
	}

	os.Setenv("DD_CUSTOM_SENSITIVE_WORDS", "")
}

func TestOnlyEnvConfigArgsScrubbingDisabled(t *testing.T) {
	os.Setenv("DD_SCRUB_ARGS", "false")
	os.Setenv("DD_CUSTOM_SENSITIVE_WORDS", "*password*,consul_token,*api_key")

	agentConfig, _ := NewAgentConfig(nil, nil)
	assert.Equal(t, false, agentConfig.Scrubber.Enabled)

	cases := []struct {
		cmdline       []string
		parsedCmdline []string
	}{
		{
			[]string{"spidly", "--mypasswords=123,456", "consul_token", "1234", "--dd_api_key=1234"},
			[]string{"spidly", "--mypasswords=123,456", "consul_token", "1234", "--dd_api_key=1234"},
		},
	}

	for i := range cases {
		fp := &process.FilledProcess{Cmdline: cases[i].cmdline}
		cases[i].cmdline = agentConfig.Scrubber.ScrubProcessCommand(fp)
		assert.Equal(t, cases[i].parsedCmdline, cases[i].cmdline)
	}

	os.Setenv("DD_SCRUB_ARGS", "")
	os.Setenv("DD_CUSTOM_SENSITIVE_WORDS", "")
}

func TestConfigNewIfExists(t *testing.T) {
	// The file does not exist: no error returned
	conf, err := NewIfExists("/does-not-exist")
	assert.Nil(t, err)
	assert.Nil(t, conf)

	// The file exists but cannot be read for another reason: an error is
	// returned.
	var filename string
	if runtime.GOOS != "windows" {

		//go doesn't honor the file permissions, so skip this test on Windows

		filename = "/tmp/process-agent-test-config.ini"
		os.Remove(filename)
		f, err := os.OpenFile(filename, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0200) // write only
		assert.Nil(t, err)
		f.Close()
		conf, err = NewIfExists(filename)
		assert.NotNil(t, err)
		assert.Nil(t, conf)
		//os.Remove(filename)
	}
}

func TestGetHostname(t *testing.T) {
	cfg := NewDefaultAgentConfig()
	h, err := getHostname(cfg.DDAgentPy, cfg.DDAgentBin, cfg.DDAgentPyEnv)
	assert.Nil(t, err)
	assert.NotEqual(t, "", h)
}

func TestDDAgentMultiAPIKeys(t *testing.T) {
	assert := assert.New(t)
	ddAgentConf, _ := ini.Load([]byte("[Main]\n\napi_key=foo, bar "))
	configFile := &File{instance: ddAgentConf, Path: "whatever"}

	agentConfig, _ := NewAgentConfig(configFile, nil)
	assert.Equal("foo", agentConfig.APIKey)
}

func TestDefaultConfig(t *testing.T) {
	assert := assert.New(t)
	agentConfig := NewDefaultAgentConfig()

	// assert that some sane defaults are set
	assert.Equal("info", agentConfig.LogLevel)
	assert.Equal(true, agentConfig.AllowRealTime)
	assert.Equal(containerChecks, agentConfig.EnabledChecks)
	assert.Equal(true, agentConfig.Scrubber.Enabled)

	os.Setenv("DOCKER_DD_AGENT", "yes")
	agentConfig = NewDefaultAgentConfig()
	assert.Equal(os.Getenv("HOST_PROC"), "")
	assert.Equal(os.Getenv("HOST_SYS"), "")
	os.Setenv("DOCKER_DD_AGENT", "no")
	assert.Equal(containerChecks, agentConfig.EnabledChecks)
}

func TestDDAgentConfigWithNewOpts(t *testing.T) {
	assert := assert.New(t)
	// Check that providing process.* options in the dd-agent conf file works
	dd, _ := ini.Load([]byte(strings.Join([]string{
		"[Main]",
		"hostname = thing",
		"api_key = apikey_12",
		"[process.config]",
		"queue_size = 5",
		"allow_real_time = false",
		"windows_args_refresh_interval = 20",
	}, "\n")))

	conf := &File{instance: dd, Path: "whatever"}
	agentConfig, err := NewAgentConfig(conf, nil)
	assert.NoError(err)

	assert.Equal("apikey_12", agentConfig.APIKey)
	assert.Equal(5, agentConfig.QueueSize)
	assert.Equal(false, agentConfig.AllowRealTime)
	assert.Equal(containerChecks, agentConfig.EnabledChecks)
	assert.Equal(20, agentConfig.Windows.ArgsRefreshInterval)
	assert.Equal(true, agentConfig.Windows.AddNewArgs)
	assert.Equal(true, agentConfig.Scrubber.Enabled)
}

func TestDDAgentConfigBothVersions(t *testing.T) {
	assert := assert.New(t)
	// Check that providing process.* options in the dd-agent conf file works
	dd, _ := ini.Load([]byte(strings.Join([]string{
		"[Main]",
		"hostname = thing",
		"api_key = apikey_12",
		"[process.config]",
		"queue_size = 5",
		"allow_real_time = false",
		"windows_args_refresh_interval = 30",
	}, "\n")))

	var ddy *YamlAgentConfig
	err := yaml.Unmarshal([]byte(strings.Join([]string{
		"api_key: apikey_20",
		"process_config:",
		"  process_dd_url: http://my-process-app.datadoghq.com",
		"  queue_size: 10",
		"  windows:",
		"    args_refresh_interval: 40",
	}, "\n")), &ddy)
	assert.NoError(err)

	conf := &File{instance: dd, Path: "whatever"}
	agentConfig, err := NewAgentConfig(conf, ddy)
	assert.NoError(err)

	assert.Equal("apikey_20", agentConfig.APIKey)
	assert.Equal("my-process-app.datadoghq.com", agentConfig.APIEndpoint.Hostname())
	assert.Equal(10, agentConfig.QueueSize)
	assert.Equal(false, agentConfig.AllowRealTime)
	assert.Equal(containerChecks, agentConfig.EnabledChecks)
	assert.Equal(40, agentConfig.Windows.ArgsRefreshInterval)
	assert.Equal(true, agentConfig.Windows.AddNewArgs)
	assert.Equal(true, agentConfig.Scrubber.Enabled)
}

func TestDDAgentConfigYamlOnly(t *testing.T) {
	assert := assert.New(t)
	var ddy YamlAgentConfig
	err := yaml.Unmarshal([]byte(strings.Join([]string{
		"api_key: apikey_20",
		"process_agent_enabled: true",
		"process_config:",
		"  enabled: 'true'",
		"  process_dd_url: http://my-process-app.datadoghq.com",
		"  queue_size: 10",
		"  intervals:",
		"    container: 8",
		"    process: 30",
		"  windows:",
		"    args_refresh_interval: 100",
		"    add_new_args: false",
		"  scrub_args: false",
	}, "\n")), &ddy)
	assert.NoError(err)

	agentConfig, err := NewAgentConfig(nil, &ddy)
	assert.NoError(err)

	assert.Equal("apikey_20", agentConfig.APIKey)
	assert.Equal("my-process-app.datadoghq.com", agentConfig.APIEndpoint.Hostname())
	assert.Equal(10, agentConfig.QueueSize)
	assert.Equal(true, agentConfig.AllowRealTime)
	assert.Equal(true, agentConfig.Enabled)
	assert.Equal(processChecks, agentConfig.EnabledChecks)
	assert.Equal(8*time.Second, agentConfig.CheckIntervals["container"])
	assert.Equal(30*time.Second, agentConfig.CheckIntervals["process"])
	assert.Equal(100, agentConfig.Windows.ArgsRefreshInterval)
	assert.Equal(false, agentConfig.Windows.AddNewArgs)
	assert.Equal(false, agentConfig.Scrubber.Enabled)

	ddy = YamlAgentConfig{}
	err = yaml.Unmarshal([]byte(strings.Join([]string{
		"api_key: apikey_20",
		"process_agent_enabled: true",
		"process_config:",
		"  enabled: 'false'",
		"  process_dd_url: http://my-process-app.datadoghq.com",
		"  queue_size: 10",
		"  intervals:",
		"    container: 8",
		"    process: 30",
		"  windows:",
		"    args_refresh_interval: -1",
		"    add_new_args: true",
		"  scrub_args: true",
	}, "\n")), &ddy)
	assert.NoError(err)

	agentConfig, err = NewAgentConfig(nil, &ddy)
	assert.NoError(err)
	assert.Equal("apikey_20", agentConfig.APIKey)
	assert.Equal("my-process-app.datadoghq.com", agentConfig.APIEndpoint.Hostname())
	assert.Equal(true, agentConfig.Enabled)
	assert.Equal(containerChecks, agentConfig.EnabledChecks)
	assert.Equal(-1, agentConfig.Windows.ArgsRefreshInterval)
	assert.Equal(true, agentConfig.Windows.AddNewArgs)
	assert.Equal(true, agentConfig.Scrubber.Enabled)

	ddy = YamlAgentConfig{}
	err = yaml.Unmarshal([]byte(strings.Join([]string{
		"api_key: apikey_20",
		"process_agent_enabled: true",
		"process_config:",
		"  enabled: 'disabled'",
		"  process_dd_url: http://my-process-app.datadoghq.com",
		"  queue_size: 10",
		"  intervals:",
		"    container: 8",
		"    process: 30",
	}, "\n")), &ddy)
	assert.NoError(err)

	agentConfig, err = NewAgentConfig(nil, &ddy)
	assert.NoError(err)
	assert.Equal("apikey_20", agentConfig.APIKey)
	assert.Equal("my-process-app.datadoghq.com", agentConfig.APIEndpoint.Hostname())
	assert.Equal(false, agentConfig.Enabled)
	assert.Equal(containerChecks, agentConfig.EnabledChecks)
	assert.Equal(15, agentConfig.Windows.ArgsRefreshInterval)
	assert.Equal(true, agentConfig.Windows.AddNewArgs)
	assert.Equal(true, agentConfig.Scrubber.Enabled)
}

func TestProxyEnv(t *testing.T) {
	assert := assert.New(t)
	for i, tc := range []struct {
		host     string
		port     int
		user     string
		pass     string
		expected string
	}{
		{
			"example.com",
			1234,
			"",
			"",
			"http://example.com:1234",
		},
		{
			"https://example.com",
			4567,
			"foo",
			"bar",
			"https://foo:bar@example.com:4567",
		},
		{
			"example.com",
			0,
			"foo",
			"",
			"http://foo@example.com:3128",
		},
	} {
		os.Setenv("PROXY_HOST", tc.host)
		if tc.port > 0 {
			os.Setenv("PROXY_PORT", strconv.Itoa(tc.port))
		} else {
			os.Setenv("PROXY_PORT", "")
		}
		os.Setenv("PROXY_USER", tc.user)
		os.Setenv("PROXY_PASSWORD", tc.pass)
		pf, err := proxyFromEnv(nil)
		assert.NoError(err, "proxy case %d had error", i)
		u, err := pf(&http.Request{})
		assert.NoError(err)
		assert.Equal(tc.expected, u.String())
	}
}

func getURL(f *ini.File) (*url.URL, error) {
	conf := File{
		f,
		"some/path",
	}
	m, _ := conf.GetSection("Main")
	pf, err := getProxySettings(m)
	if err != nil {
		return nil, err
	}
	return pf(&http.Request{})
}

func TestGetProxySettings(t *testing.T) {
	assert := assert.New(t)

	f, _ := ini.Load([]byte("[Main]\n\nproxy_host = myhost"))

	s, err := getURL(f)
	assert.NoError(err)
	assert.Equal("http://myhost:3128", s.String())

	f, _ = ini.Load([]byte("[Main]\n\nproxy_host = http://myhost"))

	s, err = getURL(f)
	assert.NoError(err)
	assert.Equal("http://myhost:3128", s.String())

	f, _ = ini.Load([]byte("[Main]\n\nproxy_host = https://myhost"))

	s, err = getURL(f)
	assert.NoError(err)
	assert.Equal("https://myhost:3128", s.String())

	// generic user name
	f, _ = ini.Load([]byte(strings.Join([]string{
		"[Main]",
		"proxy_host = https://myhost",
		"proxy_port = 3129",
		"proxy_user = aaditya",
	}, "\n")))

	s, err = getURL(f)
	assert.NoError(err)

	assert.Equal("https://aaditya@myhost:3129", s.String())

	// special char in user name <3
	f, _ = ini.Load([]byte(strings.Join([]string{
		"[Main]",
		"proxy_host = myhost",
		"proxy_port = 3129",
		"proxy_user = léo",
	}, "\n")))

	s, err = getURL(f)
	assert.NoError(err)

	// user is url-encoded and decodes to original string
	assert.Equal("http://l%C3%A9o@myhost:3129", s.String())
	assert.Equal("léo", s.User.Username())

	// generic  user-pass
	f, _ = ini.Load([]byte(strings.Join([]string{
		"[Main]",
		"proxy_host = myhost",
		"proxy_port = 3129",
		"proxy_user = aaditya",
		"proxy_password = password_12",
	}, "\n")))

	s, err = getURL(f)
	assert.NoError(err)
	assert.Equal("http://aaditya:password_12@myhost:3129", s.String())

	// user-pass with schemed host
	f, _ = ini.Load([]byte(strings.Join([]string{
		"[Main]",
		"proxy_host = https://myhost",
		"proxy_port = 3129",
		"proxy_user = aaditya",
		"proxy_password = password_12",
	}, "\n")))

	s, err = getURL(f)
	assert.NoError(err)
	assert.Equal("https://aaditya:password_12@myhost:3129", s.String())

	// special characters in password
	f, _ = ini.Load([]byte(strings.Join([]string{
		"[Main]",
		"proxy_host = https://myhost",
		"proxy_port = 3129",
		"proxy_user = aaditya",
		"proxy_password = /:!?&=@éÔγλῶσσα",
	}, "\n")))

	s, err = getURL(f)
	assert.NoError(err)

	// password is url-encoded and decodes to the original string
	assert.Equal("https://aaditya:%2F%3A%21%3F&=%40%C3%A9%C3%94%CE%B3%CE%BB%E1%BF%B6%CF%83%CF%83%CE%B1@myhost:3129", s.String())

	pass, _ := s.User.Password()
	assert.Equal("/:!?&=@éÔγλῶσσα", pass)
}

func TestIsAffirmative(t *testing.T) {
	value, err := isAffirmative("yes")
	assert.Nil(t, err)
	assert.True(t, value)

	value, err = isAffirmative("True")
	assert.Nil(t, err)
	assert.True(t, value)

	value, err = isAffirmative("1")
	assert.Nil(t, err)
	assert.True(t, value)

	_, err = isAffirmative("")
	assert.NotNil(t, err)

	value, err = isAffirmative("ok")
	assert.Nil(t, err)
	assert.False(t, value)
}
