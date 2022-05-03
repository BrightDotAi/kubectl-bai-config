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
}

func main() {
	p := tea.NewProgram(initialModel())
	if err := p.Start(); err != nil {
		fmt.Printf("Alas, there's been an error: %v", err)
		os.Exit(1)
	}
}

func initialModel() model {
	// Login To Spacelift
	storedCredentials := spacectlSession.StoredCredentials{
		Type:     spacectlSession.CredentialsTypeAPIToken,
		Endpoint: SPACELIFT_ENDPOINT,
	}
	profile.LoginUsingWebBrowser(&storedCredentials)
	if err := authenticated.Ensure(storedCredentials); err != nil {
		fmt.Printf("Could not login to Spacelift: %v", err)
		os.Exit(1)
	}

	query, err := stack.GetStackOutputs()
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

	return model{
		view:                   ClusterSelectView,
		client:                 authenticated.Client,
		clusters:               clusters,
		app_oauth_client_id:    app_oauth_client_id,
		auth_server_issuer_url: auth_server_issuer_url,
		selected:               make(map[int]struct{}),
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
			case tea.KeyEnter:
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

		// Iterate over our clusters
		for i, cluster := range m.clusters {

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
			s += fmt.Sprintf("%s [%s] %s\n", cursor, checked, cluster.id)
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
		for _, cluster := range m.clusters {
			s += "\t" + cluster.id + "\n"
		}

		// kubeconfig path text input
		s += fmt.Sprintf("Enter the path to the kubeconfig file to write to: %s\n", m.kubeconfigPathInput.View())

		// The footer
		s += "\nPress [enter] to confirm.\n"
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
	fmt.Printf("Writing kubeconfig to %s \n", kubeconfigPath)
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
			Args: []string{
				"oidc-login",
				"get-token",
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
