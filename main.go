package main

import (
	"encoding/base64"
	"flag"
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	spacectlClient "github.com/spacelift-io/spacectl/client"
	spacectlSession "github.com/spacelift-io/spacectl/client/session"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/tools/clientcmd/api"

	"github.com/BrightDotAi/kubectl-bai-config/internal/spacelift/authenticated"
	"github.com/BrightDotAi/kubectl-bai-config/internal/spacelift/profile"
	"github.com/BrightDotAi/kubectl-bai-config/internal/spacelift/stack"
)

const (
	SPACELIFT_ENDPOINT      = "https://brightdotai.app.spacelift.io/"
	EKS_COMPONENT_LABEL     = "folder:component/eks"
	OIDC_STACK_ID           = "mgmt-gbl-corp-okta-oidc-eks-auth"
	DEFAULT_KUBECONFIG_PATH = "~/.kube/config"
)

type view uint

const (
	// InvalidView represents an invalid zero value for the
	// view.
	InvalidView view = iota
	// ClusterSelectView represents the view for selecting the kubeconfig clusters
	ClusterSelectView
	// KubeConfigPathView represents the view for inputting the kubeconfig path
	KubeConfigPathView
	// Final view before program exit
	KubeConfigWriteView
)

type cluster struct {
	id                         string
	name                       string
	certificate_authority_data []byte
	endpoint                   string
}

type model struct {
	view                   view                  // The current view
	client                 spacectlClient.Client // Spacelift Session Credentials
	app_oauth_client_id    string
	auth_server_issuer_url string
	clusters               []cluster        // clusters to add to the kubeconfig
	cursor                 int              // which cluster item our cursor is pointing at
	selected               map[int]struct{} // which cluster items are selected
	kubeconfigPathInput    textinput.Model
	height                 int // terminal height, from tea.WindowSizeMsg
}

func main() {
	authMethod := flag.String("auth", "auto", "Spacelift auth method: auto, api (spacectl profile), or browser")
	selectAll := flag.Bool("select-all", false, "pre-select all clusters")
	writeAll := flag.Bool("write-all", false, "skip the interactive UI and write all clusters")
	kubeconfigPath := flag.String("kubeconfig", DEFAULT_KUBECONFIG_PATH, "kubeconfig path to write with --write-all")
	flag.Parse()
	switch *authMethod {
	case "auto", "api", "browser":
	default:
		fmt.Printf("invalid --auth value %q (want auto, api, or browser)\n", *authMethod)
		os.Exit(1)
	}

	if *writeAll {
		m := initialModel(*authMethod, true)
		m.kubeconfigPathInput.SetValue(*kubeconfigPath)
		if err := m.writeKubeConfig(); err != nil {
			fmt.Printf("Could not write kubeconfig: %s\n", err)
			os.Exit(1)
		}
		return
	}

	p := tea.NewProgram(initialModel(*authMethod, *selectAll))
	if err := p.Start(); err != nil {
		fmt.Printf("Alas, there's been an error: %v", err)
		os.Exit(1)
	}
}

// loginUsingWebBrowser runs the interactive Spacelift browser login.
func loginUsingWebBrowser() {
	storedCredentials := spacectlSession.StoredCredentials{
		Type:     spacectlSession.CredentialsTypeAPIToken,
		Endpoint: SPACELIFT_ENDPOINT,
	}
	profile.LoginUsingWebBrowser(&storedCredentials)
	if err := authenticated.Ensure(storedCredentials); err != nil {
		fmt.Printf("Could not login to Spacelift: %v", err)
		os.Exit(1)
	}
}

// spacectlProfileCredentials returns the current spacectl profile's credentials
// when it targets SPACELIFT_ENDPOINT, nil otherwise.
func spacectlProfileCredentials() *spacectlSession.StoredCredentials {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}
	manager, err := spacectlSession.NewProfileManager(filepath.Join(home, spacectlSession.SpaceliftConfigDirectory))
	if err != nil {
		return nil
	}
	p := manager.Current()
	if p == nil || p.Credentials == nil ||
		strings.TrimRight(p.Credentials.Endpoint, "/") != strings.TrimRight(SPACELIFT_ENDPOINT, "/") {
		return nil
	}
	return p.Credentials
}

func initialModel(authMethod string, selectAll bool) model {
	// Login To Spacelift: prefer the spacectl profile, browser as fallback
	usedProfile := false
	if authMethod != "browser" {
		if creds := spacectlProfileCredentials(); creds != nil {
			if err := authenticated.Ensure(*creds); err == nil {
				fmt.Println("Using spacectl profile credentials")
				usedProfile = true
			} else if authMethod == "api" {
				fmt.Printf("Could not login with the spacectl profile: %v\n", err)
				os.Exit(1)
			}
		} else if authMethod == "api" {
			fmt.Printf("--auth=api: no spacectl profile for %s — run `spacectl profile login` first\n", SPACELIFT_ENDPOINT)
			os.Exit(1)
		}
	}
	if !usedProfile {
		loginUsingWebBrowser()
	}

	query, err := stack.GetStackOutputs()
	if err != nil && usedProfile && authMethod == "auto" {
		// profile token may be stale (browser-type profiles) — retry via browser
		loginUsingWebBrowser()
		query, err = stack.GetStackOutputs()
	}
	if err != nil {
		fmt.Printf("Could not get stack outputs: %v", err)
		os.Exit(1)
	}

	app_oauth_client_id, auth_server_issuer_url, err := parseOidcStackOutputs(query.Stacks)
	if err != nil {
		fmt.Printf("Could not parse OIDC stack outputs: %v", err)
		os.Exit(1)
	}

	clusters, err := parseClusterStackOutputs(query.Stacks)
	if err != nil {
		fmt.Printf("Could not parse cluster stack outputs: %v", err)
		os.Exit(1)
	}

	ti := textinput.New()
	ti.Placeholder = DEFAULT_KUBECONFIG_PATH
	ti.Focus()
	ti.CharLimit = 1024
	ti.Width = 20

	selected := make(map[int]struct{})
	if selectAll {
		for i := range clusters {
			selected[i] = struct{}{}
		}
	}

	return model{
		view:                   ClusterSelectView,
		client:                 authenticated.Client,
		clusters:               clusters,
		app_oauth_client_id:    app_oauth_client_id,
		auth_server_issuer_url: auth_server_issuer_url,
		selected:               selected,
		kubeconfigPathInput:    ti,
	}
}

func (m model) Init() tea.Cmd {
	// Just return `nil`, which means "no I/O right now, please."
	return nil
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	cmd = nil

	if size, ok := msg.(tea.WindowSizeMsg); ok {
		m.height = size.Height
		return m, nil
	}

	switch m.view {
	case ClusterSelectView:
		switch msg := msg.(type) {

		// Is it a key press?
		case tea.KeyMsg:

			// Cool, what was the actual key pressed?
			switch msg.String() {

			// These keys should exit the program.
			case "ctrl+c", "q":
				return m, tea.Quit

				// The "up" and "k" keys move the cursor up
			case "up", "k":
				if m.cursor > 0 {
					m.cursor--
				}

			// The "down" and "j" keys move the cursor down
			case "down", "j":
				if m.cursor < len(m.clusters)-1 {
					m.cursor++
				}

			// The "right" and spacebar (a literal space) toggle
			// the selected state for the item that the cursor is pointing at.
			case "right", " ":
				_, ok := m.selected[m.cursor]
				if ok {
					delete(m.selected, m.cursor)
				} else {
					m.selected[m.cursor] = struct{}{}
				}

			case "enter":
				m.view = KubeConfigPathView
				var selectedClusters []cluster
				for i, cluster := range m.clusters {
					if _, ok := m.selected[i]; ok {
						selectedClusters = append(selectedClusters, cluster)
					}
				}
				m.clusters = selectedClusters
				cmd = textinput.Blink
			}

		}
	case KubeConfigPathView:
		switch msg := msg.(type) {
		case tea.KeyMsg:
			switch msg.Type {
			case tea.KeyTab, tea.KeyRight:
				if m.kubeconfigPathInput.Value() == "" {
					m.kubeconfigPathInput.SetValue(DEFAULT_KUBECONFIG_PATH)
					m.kubeconfigPathInput.CursorEnd()
				}

			case tea.KeyEnter:
				if m.kubeconfigPathInput.Value() == "" {
					// empty input previously wrote the kubeconfig to path ""
					m.kubeconfigPathInput.SetValue(DEFAULT_KUBECONFIG_PATH)
				}
				fmt.Printf("SELECTED PATH: %s", m.kubeconfigPathInput.Value())
				m.view = KubeConfigWriteView

			case tea.KeyCtrlC, tea.KeyEsc:
				return m, tea.Quit
			}
		}
		m.kubeconfigPathInput, cmd = m.kubeconfigPathInput.Update(msg)

	case KubeConfigWriteView:
		err := m.writeKubeConfig()
		if err != nil {
			fmt.Printf("Could not write kubeconfig: %s\n", err)
			os.Exit(1)
		}
		return m, tea.Quit
	}

	// Return the updated model to the Bubble Tea runtime for processing.
	// Note that we're not returning a command.
	return m, cmd
}

func (m model) View() string {
	var s string

	switch m.view {
	case ClusterSelectView:
		// The header
		s = "\nOIDC Authentication Details:\n"
		s += fmt.Sprintf("app_oauth_client_id: %s\n", m.app_oauth_client_id)
		s += fmt.Sprintf("auth_server_issuer_url: %s\n\n", m.auth_server_issuer_url)

		s += "Use the right arrow key or spacebar to select clusters to add to the kubeconfig:\n"

		// Window the list to the terminal height — a view taller than the screen
		// makes the bubbletea inline renderer drop the lines that scroll off.
		start, end := listWindow(len(m.clusters), m.cursor, m.height)
		if start > 0 {
			s += fmt.Sprintf("  ↑ %d more\n", start)
		}
		for i := start; i < end; i++ {

			// Is the cursor pointing at this cluster?
			cursor := " " // no cursor
			if m.cursor == i {
				cursor = ">" // cursor!
			}

			// Is this cluster selected?
			checked := " " // not selected
			if _, ok := m.selected[i]; ok {
				checked = "x" // selected!
			}

			// Render the row
			s += fmt.Sprintf("%s [%s] %s\n", cursor, checked, m.clusters[i].id)
		}
		if end < len(m.clusters) {
			s += fmt.Sprintf("  ↓ %d more\n", len(m.clusters)-end)
		}

		// The footer
		s += "\nPress [enter] to confirm.\n"
		s += "\nPress [q] to quit.\n"

	case KubeConfigPathView:
		// The header
		s = "\nOIDC Authentication Details:\n"
		s += fmt.Sprintf("app_oauth_client_id: %s\n", m.app_oauth_client_id)
		s += fmt.Sprintf("auth_server_issuer_url: %s\n\n", m.auth_server_issuer_url)
		s += "Selected clusters:\n"
		_, end := listWindow(len(m.clusters), 0, m.height)
		for _, cluster := range m.clusters[:end] {
			s += "\t" + cluster.id + "\n"
		}
		if end < len(m.clusters) {
			s += fmt.Sprintf("\t… and %d more\n", len(m.clusters)-end)
		}

		// kubeconfig path text input
		s += fmt.Sprintf("Enter the path to the kubeconfig file to write to: %s\n", m.kubeconfigPathInput.View())

		// The footer
		s += "\nPress [tab] to fill the suggested path, [enter] to confirm.\n"
		s += "\nPress [CTRL+C] or [ESC] to quit.\n"
	case KubeConfigWriteView:
	}

	return s
}

func parseClusterStackOutputs(stacks []stack.StackFragment) ([]cluster, error) {
	var clusters []cluster
	var err error
	for _, stack := range stacks {
		if contains(stack.Labels, EKS_COMPONENT_LABEL) {
			cluster := cluster{}
			for _, output := range stack.Outputs {
				switch output.ID {
				case "eks_cluster_id":
					cluster.id = strings.Trim(output.Value, "\"")
				case "eks_cluster_arn":
					cluster.name = strings.Trim(output.Value, "\"")
				case "eks_cluster_endpoint":
					cluster.endpoint = strings.Trim(output.Value, "\"")
				case "eks_cluster_certificate_authority_data":
					cluster.certificate_authority_data, err = base64.StdEncoding.DecodeString(strings.Trim(output.Value, "\""))
					if err != nil {
						return clusters, err
					}
				}
			}
			clusters = append(clusters, cluster)
		}
	}

	return clusters, nil
}

func parseOidcStackOutputs(stacks []stack.StackFragment) (string, string, error) {
	app_oauth_client_id, auth_server_issuer_url := "", ""

	for _, stack := range stacks {
		if stack.ID == OIDC_STACK_ID {
			for _, output := range stack.Outputs {
				if output.ID == "app_oauth_client_id" {
					app_oauth_client_id = strings.Trim(output.Value, "\"")
				} else if output.ID == "auth_server_issuer_url" {
					auth_server_issuer_url = strings.Trim(output.Value, "\"")
				}
			}
			return app_oauth_client_id, auth_server_issuer_url, nil
		}
	}

	return app_oauth_client_id, auth_server_issuer_url, fmt.Errorf("could not find OIDC stack")
}

const (
	KUBECONFIG_OIDC_USER = "oidc"
)

func (m model) writeKubeConfig() error {
	kubeconfigPath := expandPath(m.kubeconfigPathInput.Value())
	fmt.Printf("Writing kubeconfig with %d clusters to %s\n", len(m.clusters), kubeconfigPath)
	// Construct the kubeconfig
	kubeconfig := api.NewConfig()
	kubeconfig.Kind = "Config"
	kubeconfig.APIVersion = "v1"
	kubeconfig.Preferences = api.Preferences{
		Colors: true,
	}
	kubeconfig.AuthInfos[KUBECONFIG_OIDC_USER] = &api.AuthInfo{
		Exec: &api.ExecConfig{
			APIVersion: "client.authentication.k8s.io/v1beta1",
			Command:    "kubectl",
			Env:        []api.ExecEnvVar{},
			Args: append([]string{
				"oidc-login",
				"get-token",
				"--oidc-issuer-url=" + m.auth_server_issuer_url,
				"--oidc-client-id=" + m.app_oauth_client_id,
				"--oidc-extra-scope=email",
				"--oidc-extra-scope=offline_access",
				"--oidc-extra-scope=profile",
				"--oidc-extra-scope=openid",
			}, browserCommandArgs()...),
			InteractiveMode:    api.IfAvailableExecInteractiveMode,
			ProvideClusterInfo: false,
		},
	}
	for _, cluster := range m.clusters {
		kubeconfig.Clusters[cluster.name] = &api.Cluster{
			Server:                   cluster.endpoint,
			CertificateAuthorityData: cluster.certificate_authority_data,
		}
		kubeconfig.Contexts[cluster.id] = &api.Context{
			Cluster:  cluster.name,
			AuthInfo: KUBECONFIG_OIDC_USER,
		}
	}
	kubeconfig.CurrentContext = m.clusters[0].id

	return clientcmd.WriteToFile(*kubeconfig, kubeconfigPath)
}

// browserCommandArgs writes a background-open wrapper and returns the kubelogin arg
// pointing at it (macOS only; kubelogin execs the value as a single binary, no shell).
func browserCommandArgs() []string {
	if runtime.GOOS != "darwin" {
		return nil
	}
	usr, err := user.Current()
	if err != nil {
		return nil
	}
	wrapper := filepath.Join(usr.HomeDir, ".kube", "bai-browser-open")
	script := "#!/bin/sh\n# Written by kubectl bai-config: open the OIDC login URL without stealing focus.\nexec /usr/bin/open -g \"$@\"\n"
	if err := os.MkdirAll(filepath.Dir(wrapper), 0o755); err != nil {
		return nil
	}
	if err := os.WriteFile(wrapper, []byte(script), 0o755); err != nil {
		return nil
	}
	_ = os.Chmod(wrapper, 0o755) // WriteFile perm applies only on create
	return []string{"--browser-command=" + wrapper}
}

// listWindow returns the [start, end) item range that keeps the cursor visible;
// 14 chrome lines reserved — one line over terminal height corrupts the repaint.
func listWindow(total, cursor, height int) (int, int) {
	visible := total
	if height > 0 && height-14 < visible {
		visible = height - 14
		if visible < 3 {
			visible = 3
		}
	}
	start := 0
	if cursor >= visible {
		start = cursor - visible + 1
	}
	if start+visible > total {
		start = total - visible
		if start < 0 {
			start = 0
		}
	}
	return start, start + visible
}

func contains(s []string, e string) bool {
	for _, a := range s {
		if a == e {
			return true
		}
	}
	return false
}

func expandPath(path string) string {
	usr, _ := user.Current()
	dir := usr.HomeDir

	if path == "~" {
		// In case of "~", which won't be caught by the "else if"
		path = dir
	} else if strings.HasPrefix(path, "~/") {
		// Use strings.HasPrefix so we don't match paths like
		// "/something/~/something/"
		path = filepath.Join(dir, path[2:])
	}

	return path
}
