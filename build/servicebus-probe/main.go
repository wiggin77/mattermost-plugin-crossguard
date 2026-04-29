// servicebus-probe is a readiness gate for the local Azure Service Bus
// emulator. It retries PeekMessages against a pre-configured queue until
// the emulator responds or a 60-second deadline expires.
//
// Usage (via build/integration-tests.mk):
//
//	servicebus-probe -connstr '<emulator connstr>' -queue crossguard-relay
//
// Exit 0 on first successful peek (queue found + auth OK).
// Exit 1 on deadline or fatal init failure.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"os"
	"regexp"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/messaging/azservicebus"
)

// Defensive redaction: the probe accepts a connection string on the command
// line, and SDK errors sometimes echo AMQP endpoints including SAS fragments.
// We log only to local stdout, but avoid leaking secrets when the probe is
// wired into CI or shared consoles.
var (
	probeSharedAccessKey       = regexp.MustCompile(`(?i)SharedAccessKey=[^;\s]+`)
	probeSharedAccessSig       = regexp.MustCompile(`(?i)SharedAccessSignature=[^;\s]+`)
	probeSigQuery              = regexp.MustCompile(`(?i)([?&])sig=[^&\s]+`)
)

func sanitize(err error) string {
	if err == nil {
		return ""
	}
	s := err.Error()
	s = probeSharedAccessKey.ReplaceAllString(s, "SharedAccessKey=REDACTED")
	s = probeSharedAccessSig.ReplaceAllString(s, "SharedAccessSignature=REDACTED")
	s = probeSigQuery.ReplaceAllString(s, "${1}sig=REDACTED")
	return s
}

func main() {
	connStr := flag.String("connstr", "", "Service Bus connection string (required)")
	queue := flag.String("queue", "crossguard-relay", "Queue name to peek against")
	deadline := flag.Duration("deadline", 60*time.Second, "Overall probe deadline")
	interval := flag.Duration("interval", 2*time.Second, "Retry interval")
	flag.Parse()

	if *connStr == "" {
		fmt.Fprintln(os.Stderr, "servicebus-probe: -connstr is required")
		os.Exit(1)
	}

	ctx, cancel := context.WithTimeout(context.Background(), *deadline)
	defer cancel()

	for {
		if err := probeOnce(ctx, *connStr, *queue); err == nil {
			log.Printf("servicebus-probe: emulator is ready (queue=%q)", *queue)
			return
		} else {
			log.Printf("servicebus-probe: not ready yet: %s", sanitize(err))
		}

		select {
		case <-ctx.Done():
			fmt.Fprintf(os.Stderr, "servicebus-probe: deadline reached without a successful peek\n")
			os.Exit(1)
		case <-time.After(*interval):
		}
	}
}

func probeOnce(parent context.Context, connStr, queue string) error {
	client, err := azservicebus.NewClientFromConnectionString(connStr, nil)
	if err != nil {
		return fmt.Errorf("client init: %s", sanitize(err))
	}
	defer func() {
		closeCtx, closeCancel := context.WithTimeout(context.Background(), 5*time.Second)
		_ = client.Close(closeCtx)
		closeCancel()
	}()

	receiver, err := client.NewReceiverForQueue(queue, &azservicebus.ReceiverOptions{
		ReceiveMode: azservicebus.ReceiveModePeekLock,
	})
	if err != nil {
		return fmt.Errorf("receiver init: %s", sanitize(err))
	}
	defer func() {
		closeCtx, closeCancel := context.WithTimeout(context.Background(), 5*time.Second)
		_ = receiver.Close(closeCtx)
		closeCancel()
	}()

	ctx, cancel := context.WithTimeout(parent, 5*time.Second)
	defer cancel()

	if _, err := receiver.PeekMessages(ctx, 1, nil); err != nil {
		// Distinguish genuine transport failures from the expected
		// "emulator still warming up" signal.
		var sbErr *azservicebus.Error
		if errors.As(err, &sbErr) {
			return fmt.Errorf("peek returned %s: %s", sbErr.Code, sanitize(err))
		}
		return fmt.Errorf("peek: %s", sanitize(err))
	}
	return nil
}
