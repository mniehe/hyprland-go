package hyprland

import (
	"fmt"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"
)

var c *RequestClient

type DummyClient struct {
	RequestClient
}

func init() {
	if os.Getenv("HYPRLAND_INSTANCE_SIGNATURE") != "" {
		c = MustClient()
	}
}

func genParams(param string, n int) (params []string) {
	for i := 0; i < n; i++ {
		params = append(params, param)
	}
	return params
}

func checkEnvironment(t *testing.T) {
	if c == nil {
		t.Skip("HYPRLAND_INSTANCE_SIGNATURE not set, skipping test")
	}
}

func testCommandRR(t *testing.T, command func() (RawResponse, error)) {
	testCommand(t, command, RawResponse(""))
}

func testCommand[T any](t *testing.T, command func() (T, error), emptyValue any) {
	checkEnvironment(t)
	got, err := command()
	if err != nil {
		t.Error(err)
	}
	if reflect.TypeOf(got) != reflect.TypeOf(emptyValue) {
		t.Error("got wrong type")
	}
	if reflect.DeepEqual(got, emptyValue) {
		t.Error("got empty value")
	}
	t.Logf("got: %+v", got)
}

func TestPrepareRequests(t *testing.T) {
	// test params
	tests := []struct {
		command  string
		params   []string
		expected []string
	}{
		{"command", nil, []string{"command"}},
		{"command", []string{"param0"}, []string{"command param0"}},
		{"command", []string{"param0", "param1"}, []string{"[[BATCH]]command param0;command param1;"}},
	}
	for _, tt := range tests {
		t.Run(fmt.Sprintf("tests_%v-%v", tt.command, tt.params), func(t *testing.T) {
			requests := prepareRequests(tt.command, tt.params)
			for i, e := range tt.expected {
				if string(requests[i]) != e {
					t.Errorf("got: %s, want: %s", requests[i], e)
				}
			}
		})
	}

	// test massive amount of parameters
	massTests := []struct {
		command  string
		params   []string
		expected int
	}{
		{"command", genParams("param", 5), 1},
		{"command", genParams("param", 15), 1},
		{"command", genParams("param", 30), 1},
		{"command", genParams("param", 60), 2},
		{"command", genParams("param", 90), 3},
		{"command", genParams("param", 100), 4},
	}
	for _, tt := range massTests {
		t.Run(fmt.Sprintf("mass_tests_%v-%d", tt.command, len(tt.params)), func(t *testing.T) {
			requests := prepareRequests(tt.command, tt.params)
			if len(requests) != tt.expected {
				t.Errorf("got: %d, want: %d", len(requests), tt.expected)
			}
		})
	}
}

func TestValidateResponse(t *testing.T) {
	// Dummy client to allow this test to run without Hyprland
	c := DummyClient{}

	tests := []struct {
		params    []string
		response  RawResponse
		validate  bool
		expectErr bool
	}{
		{genParams("param", 1), RawResponse("   ok   "), true, false},
		{genParams("param", 2), RawResponse("ok"), true, true},
		{genParams("param", 2), RawResponse("ok"), false, false},
		{genParams("param", 1), RawResponse("ok ok"), true, false}, // not sure about this case, will leave like this for now
		{genParams("param", 5), RawResponse(strings.Repeat("ok", 5)), true, false},
		{genParams("param", 6), RawResponse(strings.Repeat("ok", 5)), true, true},
		{genParams("param", 6), RawResponse(strings.Repeat("ok", 5)), false, false},
		{genParams("param", 10), RawResponse(strings.Repeat(" ok ", 10)), true, false},
	}
	for _, tt := range tests {
		t.Run(fmt.Sprintf("tests_%v-%v", tt.params, tt.response), func(t *testing.T) {
			c.Validate = tt.validate
			err := c.validateResponse(tt.params, tt.response)
			if tt.expectErr && err == nil {
				t.Errorf("got: %v, want error", err)
			}
			if !tt.expectErr && err != nil {
				t.Errorf("got %v, want nil", err)
			}
		})
	}
}

func TestRequest(t *testing.T) {
	testCommandRR(t, func() (RawResponse, error) {
		return c.Request([]byte("dispatch exec"))
	})
}

func TestActiveWindow(t *testing.T) {
	testCommand(t, c.ActiveWindow, Window{})
}

func TestActiveWorkspace(t *testing.T) {
	testCommand(t, c.ActiveWorkspace, Workspace{})
}

func TestBinds(t *testing.T) {
	testCommand(t, c.Binds, []Bind{})
}

func TestClients(t *testing.T) {
	testCommand(t, c.Clients, []Client{})
}

func TestCursorPos(t *testing.T) {
	testCommand(t, c.CursorPos, CursorPos{})
}

func TestDispatch(t *testing.T) {
	testCommandRR(t, func() (RawResponse, error) {
		return c.Dispatch("exec kitty sh -c 'echo Testing hyprland-go && sleep 1 && exit 0'")
	})

	if testing.Short() {
		t.Skip("skip slow test")
	}
	// Testing if we can open at least the amount of instances we asked
	// Dispatch() to open.
	// The reason this test exist is because Hyprland has a hidden
	// limitation in the total amount of batch commands you can trigger,
	// but this is not documented and it also fails silently.
	// So this test allows us to validate that the current split of
	// batch commands is working as expected.
	// See also: prepareRequests function and MAX_COMMANDS const
	const want = 35
	const retries = 15
	t.Run(fmt.Sprintf("test_opening_%d_kitty_instances", want), func(t *testing.T) {
		must1(c.Dispatch(genParams(fmt.Sprintf("exec kitty sh -c 'sleep %d && exit 0'", retries), want)...))
		awid := must1(c.ActiveWorkspace()).Id
		got := 0
		for i := 0; i < retries; i++ {
			got = 0
			time.Sleep(1 * time.Second)
			cls := must1(c.Clients())

			for _, cl := range cls {
				if cl.Workspace.Id == awid && cl.Class == "kitty" {
					got += 1
				}
			}
			if got >= want {
				t.Logf("after retries: %d, got kitty: %d, finishing test", i+1, got)
				return
			}
		}
		// after maximum amount of retries, give up
		t.Errorf("after retries: %d, got kitty: %d, want at least: %d", retries, got, want)
	})
}

func TestGetOption(t *testing.T) {
	tests := []struct{ option string }{
		{"general:border_size"},
		{"gestures:workspace_swipe"},
		{"misc:vrr"},
	}
	for _, tt := range tests {
		t.Run(fmt.Sprintf("mass_tests_%v", tt.option), func(t *testing.T) {
			testCommand(t, func() (Option, error) {
				return c.GetOption(tt.option)
			}, Option{})
		})
	}
}

func TestKeyword(t *testing.T) {
	testCommandRR(t, func() (RawResponse, error) {
		return c.Keyword("general:border_size 1", "general:border_size 5")
	})
}

func TestKill(t *testing.T) {
	if testing.Short() {
		t.Skip("skip test that kill window")
	}
	testCommandRR(t, c.Kill)
}

func TestLayers(t *testing.T) {
	testCommand(t, c.Layers, Layers{})
}

func TestMonitors(t *testing.T) {
	testCommand(t, c.Monitors, []Monitor{})
}

func TestReload(t *testing.T) {
	testCommandRR(t, c.Reload)
}

func TestSetCursor(t *testing.T) {
	testCommandRR(t, func() (RawResponse, error) {
		return c.SetCursor("Nordzy-cursors", 32)
	})
}

func TestSplash(t *testing.T) {
	testCommand(t, c.Splash, "")
}

func TestWorkspaces(t *testing.T) {
	testCommand(t, c.Workspaces, []Workspace{})
}

func TestVersion(t *testing.T) {
	testCommand(t, c.Version, Version{})
}