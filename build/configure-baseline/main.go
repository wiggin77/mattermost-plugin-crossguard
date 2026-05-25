// configure-baseline emits the PluginSettings.Plugins.crossguard config
// patch body for one side of the dual-server dev environment. Invoked by
// `make docker-deploy` once per server.
//
// The connection set comes from build/devbaseline (single source of truth
// shared with the integration tests). Output goes to stdout as a single
// JSON object suitable for `curl -X PUT /api/v4/config/patch -d @-`.
//
// The plugin stores connection lists under map[string]any plugin settings
// as JSON-encoded strings (Mattermost's plugin config type does not allow
// nested typed structures), so we marshal each list twice: once into a
// JSON array, then again as a JSON string.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"

	"github.com/MattermostFederal/mattermost-plugin-crossguard/build/devbaseline"
)

func main() {
	sideFlag := flag.String("side", "", "dev side: a or b (required)")
	// formatFlag, when non-empty, overrides message_format on every
	// outbound connection emitted in the patch. Used by the two-pass
	// `make docker-integration-test-validate-wire` target: the first
	// pass runs with the declared baseline (the format each connection
	// names in build/devbaseline), the second pass runs with
	// `-format json` so every outbound connection emits JSON. The
	// integration suite is otherwise unchanged across the two passes;
	// it picks up the format flip via the freshly-patched plugin
	// config.
	formatFlag := flag.String("format", "", "override outbound message_format: \"xml\", \"json\", or \"\" (use declared baseline)")
	flag.Parse()

	var side devbaseline.Side
	switch *sideFlag {
	case "a":
		side = devbaseline.SideA
	case "b":
		side = devbaseline.SideB
	default:
		fmt.Fprintln(os.Stderr, "configure-baseline: --side must be 'a' or 'b'")
		os.Exit(2)
	}

	switch *formatFlag {
	case "", "xml", "json":
	default:
		fmt.Fprintf(os.Stderr, "configure-baseline: --format must be \"\", \"xml\", or \"json\" (got %q)\n", *formatFlag)
		os.Exit(2)
	}

	outboundConns := devbaseline.Outbound(side)
	if *formatFlag != "" {
		applyFormatOverride(outboundConns, *formatFlag)
	}
	outbound, err := json.Marshal(outboundConns)
	if err != nil {
		fmt.Fprintf(os.Stderr, "configure-baseline: marshal outbound: %v\n", err)
		os.Exit(1)
	}
	inbound, err := json.Marshal(devbaseline.Inbound(side))
	if err != nil {
		fmt.Fprintf(os.Stderr, "configure-baseline: marshal inbound: %v\n", err)
		os.Exit(1)
	}

	patch := map[string]any{
		"PluginSettings": map[string]any{
			"Plugins": map[string]any{
				"crossguard": map[string]any{
					"outboundconnections": string(outbound),
					"inboundconnections":  string(inbound),
				},
			},
		},
	}

	if err := json.NewEncoder(os.Stdout).Encode(patch); err != nil {
		fmt.Fprintf(os.Stderr, "configure-baseline: encode patch: %v\n", err)
		os.Exit(1)
	}
}

// applyFormatOverride sets message_format=value on every outbound
// connection entry in conns. The connection list elements are
// map[string]any (the devbaseline encoding), so we mutate in place.
// Inbound connections are not touched: inbound auto-detects format
// on a per-envelope basis and ignores its message_format field.
func applyFormatOverride(conns []map[string]any, value string) {
	for _, c := range conns {
		c["message_format"] = value
	}
}
