package main

import (
	"encoding/base64"
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/tools/clientcmd/api"

	"github.com/BrightDotAi/kubectl-bai-config/internal/spacelift/authenticated"
	"github.com/BrightDotAi/kubectl-bai-config/internal/spacelift/profile"
	"github.com/BrightDotAi/kubectl-bai-config/internal/spacelift/stack"
	spacectlSession "github.com/spacelift-io/spacectl/client/session"
)

const (
	SPACELIFT_ENDPOINT  = "https://brightdotai.app.spacelift.io/"
	EKS_COMPONENT_LABEL = "folder:component/eks"
	OIDC_STACK_ID       = "mgmt-gbl-corp-okta-oidc-eks-auth"
)

func getDefaultKubeconfigPath() string {
	usr, err := user.Current()
	if err != nil {
		return filepath.Join(os.Getenv("HOME"), ".kube", "config")
	}
	return filepath.Join(usr.HomeDir, ".kube", "config")
}

var DEFAULT_KUBECONFIG_PATH = getDefaultKubeconfigPath()

type view uint

const (
	InvalidView view = iota
	ClusterSelectView
	KubeConfigPathView
	KubeConfigWriteView
)

type cluster struct {
	id                         string
	name                       string
	certificate_authority_data []byte
	endpoint                   string
}

type model struct {
	view                   view
	app_oauth_client_id    string
	auth_server_issuer_url string
	clusters               []cluster
	cursor                 int
	selected               map[int]struct{}
	kubeconfigPathInput    textinput.Model
}

func main() {
	p := tea.NewProgram(initialModel())
	if err := p.Start(); err != nil {
		fmt.Printf("Error: %v\n", err)
		os.Exit(1)
	}
}

func initialModel() model {
	// Spacelift login using API token (no browser)
	storedCredentials := spacectlSession.StoredCredentials{
		Type:     spacectlSession.CredentialsTypeAPIToken,
		Endpoint: SPACELIFT_ENDPOINT,
		APIToken: os.Getenv("SPACELIFT_API_TOKEN"), // get token from env
	}

	// Ensure the client is authenticated
	if err := authenticated.Ensure(storedCredentials); err != nil {
		fmt.Printf("Could not login to Spacelift: %v\n", err)
		os.Exit(1)
	}

	query, err := stack.GetStackOutputs()
	if err != nil {
		fmt.Printf("Could not get stack outputs: %v\n", err)
		os.Exit(1)
	}

	appID, issuerURL, err := parseOidcStackOutputs(query.Stacks)
	if err != nil {
		fmt.Printf("Could not parse OIDC stack outputs: %v\n", err)
		os.Exit(1)
	}

	clusters, err := parseClusterStackOutputs(query.Stacks)
	if err != nil {
		fmt.Printf("Could not parse cluster stack outputs: %v\n", err)
		os.Exit(1)
	}

	ti := textinput.New()
	ti.Placeholder = DEFAULT_KUBECONFIG_PATH
	ti.SetValue(DEFAULT_KUBECONFIG_PATH)
	ti.Focus()
	ti.CharLimit = 1024
	ti.Width = 40

	return model{
		view:                   ClusterSelectView,
		clusters:               clusters,
		app_oauth_client_id:    appID,
		auth_server_issuer_url: issuerURL,
		selected:               make(map[int]struct{}),
		kubeconfigPathInput:    ti,
	}
}


// --- BubbleTea Update & View logic ---

func (m model) Init() tea.Cmd { return nil }

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd

	switch m.view {
	case ClusterSelectView:
		switch msg := msg.(type) {
		case tea.KeyMsg:
			switch msg.String() {
			case "ctrl+c", "q":
				return m, tea.Quit
			case "up", "k":
				if m.cursor > 0 {
					m.cursor--
				}
			case "down", "j":
				if m.cursor < len(m.clusters)-1 {
					m.cursor++
				}
			case "right", " ":
				if _, ok := m.selected[m.cursor]; ok {
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
			case tea.KeyEnter:
				m.view = KubeConfigWriteView
			case tea.KeyCtrlC, tea.KeyEsc:
				return m, tea.Quit
			}
		}
		m.kubeconfigPathInput, cmd = m.kubeconfigPathInput.Update(msg)

	case KubeConfigWriteView:
		if err := m.writeKubeConfig(); err != nil {
			fmt.Printf("Could not write kubeconfig: %v\n", err)
			os.Exit(1)
		}
		return m, tea.Quit
	}

	return m, cmd
}

func (m model) View() string {
	var s string
	switch m.view {
	case ClusterSelectView:
		s = fmt.Sprintf("\nOIDC Client ID: %s\nIssuer URL: %s\n\n", m.app_oauth_client_id, m.auth_server_issuer_url)
		s += "Select clusters:\n"
		for i, c := range m.clusters {
			cursor := " "
			if m.cursor == i {
				cursor = ">"
			}
			checked := " "
			if _, ok := m.selected[i]; ok {
				checked = "x"
			}
			s += fmt.Sprintf("%s [%s] %s\n", cursor, checked, c.id)
		}
		s += "\nPress [enter] to continue, [q] to quit.\n"

	case KubeConfigPathView:
		s = fmt.Sprintf("\nOIDC Client ID: %s\nIssuer URL: %s\n\n", m.app_oauth_client_id, m.auth_server_issuer_url)
		s += "Selected clusters:\n"
		for _, c := range m.clusters {
			s += "\t" + c.id + "\n"
		}
		s += fmt.Sprintf("Enter kubeconfig path: %s\n", m.kubeconfigPathInput.View())

	case KubeConfigWriteView:
		s = "Writing kubeconfig...\n"
	}
	return s
}

// --- Helpers ---

func parseClusterStackOutputs(stacks []stack.StackFragment) ([]cluster, error) {
	var clusters []cluster
	for _, s := range stacks {
		if contains(s.Labels, EKS_COMPONENT_LABEL) {
			c := cluster{}
			for _, out := range s.Outputs {
				switch out.ID {
				case "eks_cluster_id":
					c.id = strings.Trim(out.Value, "\"")
				case "eks_cluster_arn":
					c.name = strings.Trim(out.Value, "\"")
				case "eks_cluster_endpoint":
					c.endpoint = strings.Trim(out.Value, "\"")
				case "eks_cluster_certificate_authority_data":
					var err error
					c.certificate_authority_data, err = base64.StdEncoding.DecodeString(strings.Trim(out.Value, "\""))
					if err != nil {
						return nil, err
					}
				}
			}
			clusters = append(clusters, c)
		}
	}
	return clusters, nil
}

func parseOidcStackOutputs(stacks []stack.StackFragment) (string, string, error) {
	for _, s := range stacks {
		if s.ID == OIDC_STACK_ID {
			var appID, issuerURL string
			for _, out := range s.Outputs {
				if out.ID == "app_oauth_client_id" {
					appID = strings.Trim(out.Value, "\"")
				} else if out.ID == "auth_server_issuer_url" {
					issuerURL = strings.Trim(out.Value, "\"")
				}
			}
			return appID, issuerURL, nil
		}
	}
	return "", "", fmt.Errorf("could not find OIDC stack")
}

const KUBECONFIG_OIDC_USER = "oidc"

func (m model) writeKubeConfig() error {
	kubeconfigPath := expandPath(m.kubeconfigPathInput.Value())
	kc := api.NewConfig()
	kc.AuthInfos[KUBECONFIG_OIDC_USER] = &api.AuthInfo{
		Exec: &api.ExecConfig{
			APIVersion: "client.authentication.k8s.io/v1beta1",
			Command:    "kubectl",
			Args: []string{
				"oidc-login", "get-token",
				"--oidc-issuer-url=" + m.auth_server_issuer_url,
				"--oidc-client-id=" + m.app_oauth_client_id,
				"--oidc-extra-scope=email",
				"--oidc-extra-scope=offline_access",
				"--oidc-extra-scope=profile",
				"--oidc-extra-scope=openid",
			},
			InteractiveMode:    api.IfAvailableExecInteractiveMode,
			ProvideClusterInfo: false,
		},
	}
	for _, c := range m.clusters {
		kc.Clusters[c.name] = &api.Cluster{
			Server:                   c.endpoint,
			CertificateAuthorityData: c.certificate_authority_data,
		}
		kc.Contexts[c.id] = &api.Context{
			Cluster:  c.name,
			AuthInfo: KUBECONFIG_OIDC_USER,
		}
	}
	if len(m.clusters) > 0 {
		kc.CurrentContext = m.clusters[0].id
	}
	return clientcmd.WriteToFile(*kc, kubeconfigPath)
}

func contains(slice []string, e string) bool {
	for _, s := range slice {
		if s == e {
			return true
		}
	}
	return false
}

func expandPath(path string) string {
	usr, _ := user.Current()
	dir := usr.HomeDir
	if path == "~" {
		return dir
	} else if strings.HasPrefix(path, "~/") {
		return filepath.Join(dir, path[2:])
	}
	return path
}
