package sql

import (
	"testing"

	claims "github.com/grafana/authlib/types"
	"github.com/stretchr/testify/require"

	"github.com/grafana/grafana/pkg/services/sqlstore/migrator"
	"github.com/grafana/grafana/pkg/setting"
	"github.com/grafana/grafana/pkg/storage/unified/resource"
)

func TestIsHighAvailabilityEnabled(t *testing.T) {
	tests := []struct {
		name string
		cfg  *setting.Cfg
		isHA bool
	}{
		{
			name: "SQLite should never have HA enabled",
			cfg: func() *setting.Cfg {
				cfg := setting.NewCfg()
				dbSection := cfg.SectionWithEnvOverrides("database")
				dbSection.Key("type").SetValue(migrator.SQLite)
				dbSection.Key("high_availability").SetValue("true")
				return cfg
			}(),
			isHA: false,
		},
		{
			name: "MySQL with HA enabled in config should default to true",
			cfg: func() *setting.Cfg {
				cfg := setting.NewCfg()
				dbSection := cfg.SectionWithEnvOverrides("database")
				dbSection.Key("type").SetValue(migrator.MySQL)
				dbSection.Key("high_availability").SetValue("true")
				return cfg
			}(),
			isHA: true,
		},
		{
			name: "MySQL with HA disabled in config should default to false",
			cfg: func() *setting.Cfg {
				cfg := setting.NewCfg()
				dbSection := cfg.SectionWithEnvOverrides("database")
				dbSection.Key("type").SetValue(migrator.MySQL)
				dbSection.Key("high_availability").SetValue("false")
				return cfg
			}(),
			isHA: false,
		},
		{
			name: "MySQL with no HA config should default to true",
			cfg: func() *setting.Cfg {
				cfg := setting.NewCfg()
				dbSection := cfg.SectionWithEnvOverrides("database")
				dbSection.Key("type").SetValue(migrator.MySQL)
				return cfg
			}(),
			isHA: true,
		},
		{
			name: "Postgres with HA enabled in config should default to true",
			cfg: func() *setting.Cfg {
				cfg := setting.NewCfg()
				dbSection := cfg.SectionWithEnvOverrides("database")
				dbSection.Key("type").SetValue(migrator.Postgres)
				dbSection.Key("high_availability").SetValue("true")
				return cfg
			}(),
			isHA: true,
		},
		{
			name: "Postgres with HA disabled in config should default to false",
			cfg: func() *setting.Cfg {
				cfg := setting.NewCfg()
				dbSection := cfg.SectionWithEnvOverrides("database")
				dbSection.Key("type").SetValue(migrator.Postgres)
				dbSection.Key("high_availability").SetValue("false")
				return cfg
			}(),
			isHA: false,
		},
		{
			name: "Postgres with no HA config should default to true",
			cfg: func() *setting.Cfg {
				cfg := setting.NewCfg()
				dbSection := cfg.SectionWithEnvOverrides("database")
				dbSection.Key("type").SetValue(migrator.Postgres)
				return cfg
			}(),
			isHA: true,
		},
		{
			name: "No database type set should default to true",
			cfg: func() *setting.Cfg {
				cfg := setting.NewCfg()
				_ = cfg.SectionWithEnvOverrides("database")
				return cfg
			}(),
			isHA: true,
		},
		{
			name: "No database type set with HA enabled in config should default to true",
			cfg: func() *setting.Cfg {
				cfg := setting.NewCfg()
				dbSection := cfg.SectionWithEnvOverrides("database")
				dbSection.Key("high_availability").SetValue("true")
				return cfg
			}(),
			isHA: true,
		},
		{
			name: "No database type set with HA disabled in config should default to false",
			cfg: func() *setting.Cfg {
				cfg := setting.NewCfg()
				dbSection := cfg.SectionWithEnvOverrides("database")
				dbSection.Key("high_availability").SetValue("false")
				return cfg
			}(),
			isHA: false,
		},
		{
			name: "Resource API with non-SQLite database type should default to true",
			cfg: func() *setting.Cfg {
				cfg := setting.NewCfg()
				dbSection := cfg.SectionWithEnvOverrides("database")
				dbSection.Key("type").SetValue(migrator.SQLite)
				resourceAPISection := cfg.SectionWithEnvOverrides("resource_api")
				resourceAPISection.Key("db_type").SetValue(migrator.Postgres)
				return cfg
			}(),
			isHA: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isHighAvailabilityEnabled(tt.cfg.SectionWithEnvOverrides("database"),
				tt.cfg.SectionWithEnvOverrides("resource_api"))
			require.Equal(t, tt.isHA, result)
		})
	}
}

func TestWithAccessClient(t *testing.T) {
	t.Run("nil access client is a no-op", func(t *testing.T) {
		resourceOpts := &resource.ResourceServerOptions{}
		require.NoError(t, withAccessClient(&ServerOptions{}, resourceOpts))
		require.Nil(t, resourceOpts.AccessClient)
	})

	t.Run("default config constructs limited client", func(t *testing.T) {
		resourceOpts := &resource.ResourceServerOptions{}
		err := withAccessClient(&ServerOptions{
			AccessClient: claims.FixedAccessClient(true),
			Cfg:          setting.NewCfg(),
		}, resourceOpts)
		require.NoError(t, err)
		require.NotNil(t, resourceOpts.AccessClient)
	})

	t.Run("invalid exemptions fail setup", func(t *testing.T) {
		cfg := setting.NewCfg()
		cfg.UnifiedStorageAuthzExemptionEnabled = true
		cfg.UnifiedStorageAuthzExemptResources = []string{"not-a-valid-entry"}
		resourceOpts := &resource.ResourceServerOptions{}
		err := withAccessClient(&ServerOptions{
			AccessClient: claims.FixedAccessClient(true),
			Cfg:          cfg,
		}, resourceOpts)
		require.Error(t, err)
		require.Nil(t, resourceOpts.AccessClient)
	})

	t.Run("always-enforced exemption fails setup", func(t *testing.T) {
		cfg := setting.NewCfg()
		cfg.UnifiedStorageAuthzExemptionEnabled = true
		cfg.UnifiedStorageAuthzExemptResources = []string{"dashboard.grafana.app/dashboards"}
		resourceOpts := &resource.ResourceServerOptions{}
		err := withAccessClient(&ServerOptions{
			AccessClient: claims.FixedAccessClient(true),
			Cfg:          cfg,
		}, resourceOpts)
		require.Error(t, err)
		require.Nil(t, resourceOpts.AccessClient)
	})

	t.Run("valid exemptions construct limited client", func(t *testing.T) {
		cfg := setting.NewCfg()
		cfg.UnifiedStorageAuthzExemptionEnabled = true
		cfg.UnifiedStorageAuthzExemptResources = []string{"playlist.grafana.app/playlists"}
		resourceOpts := &resource.ResourceServerOptions{}
		err := withAccessClient(&ServerOptions{
			AccessClient: claims.FixedAccessClient(true),
			Cfg:          cfg,
		}, resourceOpts)
		require.NoError(t, err)
		require.NotNil(t, resourceOpts.AccessClient)
	})
}
