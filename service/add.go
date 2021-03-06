package service

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"
	"strconv"
	"strings"

	"github.com/google/shlex"
	"github.com/spf13/cobra"

	"github.com/runabove/sail/internal"
)

var cmdAddLink []string
var cmdAddNetworkAllow []string
var addPublish []string
var cmdAddGateway []string
var cmdAddVolume []string
var addBatch bool
var cmdAddRedeploy bool
var cmdAddBody Add
var cmdAddNetwork []string
var cmdAddCommand string
var cmdAddEntrypoint string

const cmdAddUsage = "Invalid usage. sail service add [<application>/]<repository>[:tag] [<service>]. Please see sail service add --help"

func addCmd() *cobra.Command {

	cmd := &cobra.Command{
		Use:     "add",
		Aliases: []string{"create", "a", "c"},
		Short:   "Add a new docker service",
		Long: `add [<namespace>/]<repository>[:tag] [namespace/]<service-name>
		--model         Container model
		--number        Number of container to run
		[--link         name:alias]
		[--network      {public|private|<namespace name>}]
		[--network-allow [network:]ip[/mask] Use IPs whitelist]
		[--publish, -p  Publish a container's port to the host]
		[                 format: network:publishedPort:containerPort, network::containerPort, publishedPort:containerPort, containerPort]
		[--gateway      DEPRECATED: network-input:network-output
		[--restart {no|always[:<max>]|on-failure[:<max>]}]
		[--volume       /path:size] (Size in GB)
		[--batch        do not attach console on start]
		[--pool         deploy on dedicated host pool <name>]
		[--redeploy     if the service already exists, redeploy instead]

		override docker options:
			--user
			--entrypoint
			--command
			--workdir
			--environment KEY=val
		other options:

The command will exit as soon as all service containers have stopped.
Its exit status will be the one of the last container. If the last container was stopped with
a signal, the command exits with an exit status of 255.
		`,
		Run: cmdAdd,
	}
	cmd.Flags().StringVarP(&cmdAddBody.ContainerModel, "model", "", "x1", "Container model")
	cmd.Flags().IntVarP(&cmdAddBody.ContainerNumber, "number", "", 1, "Number of container to run")
	cmd.Flags().StringSliceVarP(&cmdAddLink, "link", "", nil, "name:alias")
	cmd.Flags().StringSliceVar(&cmdAddNetwork, "network", []string{}, "public|private|<namespace name>")
	cmd.Flags().StringSliceVar(&cmdAddNetworkAllow, "network-allow", []string{}, "[network:]ip[/mask] Use IPs whitelist")
	cmd.Flags().StringSliceVarP(&addPublish, "publish", "p", nil, "Publish a container's port to the host")
	cmd.Flags().StringSliceVar(&cmdAddGateway, "gateway", nil, "network-input:network-output")
	cmd.Flags().StringVarP(&cmdAddBody.RestartPolicy, "restart", "", "no", "{no|always[:<max>]|on-failure[:<max>]}")
	cmd.Flags().StringVarP(&cmdAddCommand, "command", "", "", "override docker run command")
	cmd.Flags().StringVarP(&cmdAddBody.RepositoryTag, "tag", "", "", "deploy from new image version")
	cmd.Flags().StringVarP(&cmdAddBody.ContainerWorkdir, "workdir", "", "", "override docker workdir")
	cmd.Flags().StringVarP(&cmdAddEntrypoint, "entrypoint", "", "", "override docker entrypoint")
	cmd.Flags().StringVarP(&cmdAddBody.ContainerUser, "user", "", "", "override docker user")
	cmd.Flags().StringSliceVar(&cmdAddVolume, "volume", nil, "/path:size] (Size in GB)")
	cmd.Flags().BoolVarP(&addBatch, "batch", "", false, "do not attach console on start")
	cmd.Flags().BoolVarP(&cmdAddRedeploy, "redeploy", "", false, "if the service already exists, redeploy instead")
	cmd.Flags().StringSliceVarP(&cmdAddBody.ContainerEnvironment, "env", "e", nil, "override docker environment. Syntax: key=val,...")
	cmd.Flags().StringVarP(&cmdAddBody.Pool, "pool", "", "", "Dedicated host pool")
	return cmd
}

// PortConfig is a parameter of Add to modify exposed container ports
type PortConfig struct {
	PublishedPort    int      `json:"published_port"`
	WhitelistedCidrs []string `json:"whitelisted_cidrs"`
	Network          string   `json:"network,omitempty"`
}

// VolumeConfig is a parameter of Add to modify mounted volumes
type VolumeConfig struct {
	Size string `json:"size"`
}

// Add struct holds all parameters sent to /applications/%s/services/%s
type Add struct {
	Service              string                         `json:"-"`
	Volumes              map[string]VolumeConfig        `json:"volumes,omitempty"`
	Repository           string                         `json:"repository"`
	ContainerUser        string                         `json:"container_user,omitempty"`
	RestartPolicy        string                         `json:"restart_policy"`
	ContainerCommand     []string                       `json:"container_command,omitempty"`
	ContainerNetwork     map[string]map[string][]string `json:"container_network"`
	ContainerEntrypoint  []string                       `json:"container_entrypoint,omitempty"`
	ContainerNumber      int                            `json:"container_number"`
	RepositoryTag        string                         `json:"repository_tag"`
	Links                map[string]string              `json:"links"`
	Application          string                         `json:"namespace"`
	ContainerWorkdir     string                         `json:"container_workdir,omitempty"`
	ContainerEnvironment []string                       `json:"container_environment"`
	ContainerModel       string                         `json:"container_model"`
	ContainerPorts       map[string][]PortConfig        `json:"container_ports"`
	Pool                 string                         `json:"pool,omitempty"`
}

func cmdAdd(cmd *cobra.Command, args []string) {
	cmdAddBody.ContainerNetwork = make(map[string]map[string][]string)
	cmdAddBody.Links = make(map[string]string)
	cmdAddBody.ContainerPorts = make(map[string][]PortConfig)

	if len(args) > 2 || len(args) < 1 {
		fmt.Fprintln(os.Stderr, cmdAddUsage)
		os.Exit(1)
	}

	// Split namespace and repository
	host, app, repo, tag, err := internal.ParseResourceName(args[0])
	internal.Check(err)
	cmdAddBody.Application = app
	cmdAddBody.Repository = repo
	cmdAddBody.RepositoryTag = tag

	if !internal.CheckHostConsistent(host) {
		fmt.Fprintf(os.Stderr, "Error: Invalid Host %s for endpoint %s\n", host, internal.Host)
		os.Exit(1)
	}

	// Service name
	if len(args) > 1 {
		cmdAddBody.Service = args[1]
	} else {
		cmdAddBody.Service = cmdAddBody.Repository
	}

	// Sanity checks
	err = internal.CheckName(cmdAddBody.Application)
	internal.Check(err)
	err = internal.CheckName(cmdAddBody.Repository)
	internal.Check(err)
	err = internal.CheckName(cmdAddBody.Service)
	internal.Check(err)

	serviceAdd(cmdAddBody)
}

func serviceAdd(args Add) {

	if args.ContainerEnvironment == nil {
		args.ContainerEnvironment = make([]string, 0)
	}

	// Parse command
	if cmdAddCommand != "" {
		command, err := shlex.Split(cmdAddCommand)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Fatal, cannot split command %s\n", err)
			return
		}
		args.ContainerCommand = command
	}

	// Parse Entrypoint
	if cmdAddEntrypoint != "" {
		entrypoint, err := shlex.Split(cmdAddEntrypoint)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Fatal, cannot split command %s\n", err)
			return
		}
		args.ContainerEntrypoint = entrypoint
	}

	// Parse volumes
	if len(cmdAddVolume) > 0 {
		args.Volumes = make(map[string]VolumeConfig)
	}
	for _, vol := range cmdAddVolume {
		t := strings.Split(vol, ":")
		if len(t) == 2 {
			args.Volumes[t[0]] = VolumeConfig{Size: t[1]}
		} else if len(t) == 1 {
			args.Volumes[t[0]] = VolumeConfig{Size: "10"}
		} else {
			fmt.Fprintf(os.Stderr, "Error: Volume parameter '%s' not formated correctly\n", vol)
			os.Exit(1)
		}
	}

	// Parse links
	if len(redeployLink) > 0 {
		args.Links = make(map[string]string)
	}

	for _, link := range cmdAddLink {
		t := strings.Split(link, ":")
		if len(t) == 1 {
			args.Links[t[0]] = t[0]
		} else {
			args.Links[t[0]] = t[1]
		}
	}

	// Parse ContainerNetworks arguments
	for _, network := range cmdAddNetwork {
		args.ContainerNetwork[network] = make(map[string][]string)
	}

	for _, gat := range cmdAddGateway {
		fmt.Fprintf(os.Stderr, "WARNING: --gateway parameter is deprecated")

		t := strings.Split(gat, ":")
		if len(t) != 2 {
			fmt.Fprintf(os.Stderr, "Invalid gateway parameter, should be \"input:output\". Typically, output will be one of 'predictor', 'public'")
			os.Exit(1)
		}
		if _, ok := args.ContainerNetwork[t[0]]; !ok {
			fmt.Fprintf(os.Stderr, "Automatically adding %s to network list\n", t[0])
			args.ContainerNetwork[t[0]] = make(map[string][]string)
		}
		if _, ok := args.ContainerNetwork[t[1]]; !ok {
			fmt.Fprintf(os.Stderr, "Automatically adding %s to network list\n", t[0])
			args.ContainerNetwork[t[1]] = make(map[string][]string)
		}
		args.ContainerNetwork[t[0]]["gateway_to"] = append(args.ContainerNetwork[t[0]]["gateway_to"], t[1])
	}

	// Parse ContainerPorts
	args.ContainerPorts = parsePublishedPort(addPublish)

	// Parse NetworkAllow
	args.ContainerPorts = parseWhitelistedCidrs(cmdAddNetworkAllow, args.ContainerPorts)

	path := fmt.Sprintf("/applications/%s/services/%s", args.Application, args.Service)
	body, err := json.MarshalIndent(args, " ", " ")
	if err != nil {
		fmt.Fprintf(os.Stderr, "Fatal: %s\n", err)
		return
	}

	buffer, code, err := internal.Stream("POST", path, body)

	// http.Request failed for some reason
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %s\n", err)
		return
	}

	//  If we are in ensure mode, fallback to redeploy
	if code == 409 && cmdAddRedeploy {
		ensureMode(args)
		return
	} else if code >= 400 {
		body, err = ioutil.ReadAll(buffer)
		internal.Check(err)
		internal.FormatOutputError(body)
		return
	}

	line, err := internal.DisplayStream(buffer)

	//  If we are in ensure mode, fallback to redeploy
	if err != nil {
		e := internal.DecodeError(line)
		if e != nil && e.Code == 409 && cmdAddRedeploy {
			ensureMode(args)
			return
		}
		internal.FormatOutputError(line)
		return
	}

	// Always start service
	if internal.Format == "pretty" {
		fmt.Fprintf(os.Stderr, "Starting service %s/%s...\n", args.Application, args.Service)
	}
	serviceStart(args.Application, args.Service, addBatch)
}

func ensureMode(args Add) {
	redeployBatch = addBatch
	redeployBody := Redeploy{
		Service:              args.Service,
		Volumes:              args.Volumes,
		Repository:           args.Repository,
		ContainerUser:        args.ContainerUser,
		RestartPolicy:        args.RestartPolicy,
		ContainerCommand:     args.ContainerCommand,
		ContainerNetwork:     args.ContainerNetwork,
		ContainerEntrypoint:  args.ContainerEntrypoint,
		RepositoryTag:        args.RepositoryTag,
		Links:                args.Links,
		Application:          args.Application,
		ContainerWorkdir:     args.ContainerWorkdir,
		ContainerEnvironment: args.ContainerEnvironment,
		ContainerModel:       args.ContainerModel,
		ContainerPorts:       args.ContainerPorts,
	}
	doServiceRedeploy(redeployBody, args.Application, args.Service)
}

func parsePort(raw string) (int, error) {
	port, err := strconv.Atoi(raw)
	if err != nil || port < 1 || port > 65535 {
		return -1, fmt.Errorf("Invalid port number '%s': should be between 1 and 65535", raw)
	}
	return port, nil
}

func parsePublishedPort(args []string) map[string][]PortConfig {
	v := make(map[string][]PortConfig)

	for _, pub := range args {
		split := strings.Split(pub, ":")
		if len(split) == 1 { // containerPort
			port, err := parsePort(split[0])
			internal.Check(err)
			v[split[0]+"/tcp"] = []PortConfig{PortConfig{PublishedPort: port}}
		} else if len(split) == 2 { // network:containerPort, publishedPort:containerPort
			port, err := strconv.Atoi(split[0])
			if err != nil { // network:containerPort
				key := split[1] + "/tcp"
				port, err = parsePort(split[1])
				internal.Check(err)
				v[key] = append(v[key], PortConfig{PublishedPort: port, Network: split[0]})
			} else { // publishedPort:containerPort
				key := split[1] + "/tcp"
				port, err = parsePort(split[0])
				internal.Check(err)
				v[key] = append(v[key], PortConfig{PublishedPort: port})
			}
		} else if len(split) == 3 { // network:publishedPort:containerPort, network::containerPort
			if split[1] == "" {
				split[1] = split[2]
			}

			port, err := parsePort(split[1])
			internal.Check(err)

			key := split[2] + "/tcp"
			v[key] = append(v[key], PortConfig{PublishedPort: port, Network: split[0]})
		} else {
			fmt.Fprintf(os.Stderr, "Error: Invalid port expose rule '%s'\n", pub)
			os.Exit(1)
		}
	}

	return v
}

func parseWhitelistedCidrs(args []string, containerPorts map[string][]PortConfig) map[string][]PortConfig {
	// Parse NetworkAllow
	for _, network := range args {
		parsedNetwork := strings.Split(network, ":")
		addr := parsedNetwork[0]
		if len(parsedNetwork) == 1 {
			// No port specified, applying to all exposed ports
			for port := range containerPorts {
				for portConfig := range containerPorts[port] {
					containerPorts[port][portConfig].WhitelistedCidrs = append(containerPorts[port][portConfig].WhitelistedCidrs, addr)
				}
			}
		} else if len(parsedNetwork) == 2 {
			// Apply to specified port
			port := parsedNetwork[1] + "/tcp"
			for portConfig := range containerPorts[port] {
				containerPorts[port][portConfig].WhitelistedCidrs = append(containerPorts[port][portConfig].WhitelistedCidrs, addr)
			}
		} else {
			fmt.Fprintf(os.Stderr, "Invalid allowed network, should be 1.2.3.4[/24][:80]")
			os.Exit(1)
		}
	}

	return containerPorts
}
