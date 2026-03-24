package setup

import (
	"context"

	"github.com/cloudygreybeard/jumpgate/internal/config"
	"github.com/cloudygreybeard/jumpgate/internal/hooks"
)

func SetupCredentials(ctx context.Context, rc *config.ResolvedContext) error {
	_, err := hooks.RunRequired(ctx, rc, "setup-credentials")
	return err
}
