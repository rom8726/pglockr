package pg

import (
	"context"
	"fmt"
)

// Capabilities describes whether the connected role has the privileges pglockr
// needs: reading other backends' query texts (pg_monitor / pg_read_all_stats)
// and signalling backends (pg_signal_backend). A superuser has both implicitly.
type Capabilities struct {
	Role         string
	IsSuperuser  bool
	CanReadStats bool // effective: superuser or member of pg_read_all_stats/pg_monitor
	CanSignal    bool // effective: superuser or member of pg_signal_backend
}

// Capabilities inspects the current role's effective privileges. pg_has_role
// resolves membership transitively, so membership of pg_monitor (which contains
// pg_read_all_stats) is detected via the pg_read_all_stats check.
func (c *Client) Capabilities(ctx context.Context) (Capabilities, error) {
	ctx, cancel := context.WithTimeout(ctx, c.statementTimeout)
	defer cancel()

	const q = `
SELECT
    current_user,
    r.rolsuper,
    pg_has_role(current_user, 'pg_read_all_stats', 'MEMBER'),
    pg_has_role(current_user, 'pg_signal_backend', 'MEMBER')
FROM pg_roles r
WHERE r.rolname = current_user`

	var (
		caps             Capabilities
		hasStats, hasSig bool
	)
	if err := c.pool.QueryRow(ctx, q).Scan(&caps.Role, &caps.IsSuperuser, &hasStats, &hasSig); err != nil {
		return Capabilities{}, fmt.Errorf("inspect role capabilities: %w", err)
	}
	caps.CanReadStats = caps.IsSuperuser || hasStats
	caps.CanSignal = caps.IsSuperuser || hasSig
	return caps, nil
}
