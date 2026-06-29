package theme

import (
	"charm.land/lipgloss/v2"
	"charm.land/lipgloss/v2/compat"
	"charm.land/log/v2"

	"github.com/dlvhdr/gh-dash/v4/internal/config"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/constants"
)

type Theme struct {
	SelectedBackground      compat.AdaptiveColor // config.Theme.Colors.Background.Selected
	ActiveBackground        compat.AdaptiveColor // config.Theme.Colors.Background.Active
	DiffAddedBg             compat.AdaptiveColor // config.Theme.Colors.Diff.Added
	DiffRemovedBg           compat.AdaptiveColor // config.Theme.Colors.Diff.Removed
	DiffHeaderBg            compat.AdaptiveColor // config.Theme.Colors.Diff.Header
	DiffContextBg           compat.AdaptiveColor // config.Theme.Colors.Diff.Context
	DiffText                compat.AdaptiveColor // config.Theme.Colors.Diff.Text
	PrimaryBorder           compat.AdaptiveColor // config.Theme.Colors.Border.Primary
	FaintBorder             compat.AdaptiveColor // config.Theme.Colors.Border.Faint
	SecondaryBorder         compat.AdaptiveColor // config.Theme.Colors.Border.Secondary
	FaintText               compat.AdaptiveColor // config.Theme.Colors.Text.Faint
	PrimaryText             compat.AdaptiveColor // config.Theme.Colors.Text.Primary
	SecondaryText           compat.AdaptiveColor // config.Theme.Colors.Text.Secondary
	InvertedText            compat.AdaptiveColor // config.Theme.Colors.Text.Inverted
	SuccessText             compat.AdaptiveColor // config.Theme.Colors.Text.Success
	WarningText             compat.AdaptiveColor // config.Theme.Colors.Text.Warning
	ErrorText               compat.AdaptiveColor // config.Theme.Colors.Text.Error
	ActorText               compat.AdaptiveColor // config.Theme.Colors.Text.Actor
	NewContributorIconColor compat.AdaptiveColor // config.Theme.Colors.Icon.NewContributor
	ContributorIconColor    compat.AdaptiveColor // config.Theme.Colors.Icon.Contributor
	CollaboratorIconColor   compat.AdaptiveColor // config.Theme.Colors.Icon.Collaborator
	MemberIconColor         compat.AdaptiveColor // config.Theme.Colors.Icon.Member
	OwnerIconColor          compat.AdaptiveColor // config.Theme.Colors.Icon.Owner
	UnknownRoleIconColor    compat.AdaptiveColor // config.Theme.Colors.Icon.UnknownRole
	NewContributorIcon      string               // config.Theme.Icons.NewContributor
	ContributorIcon         string               // config.Theme.Icons.Contributor
	CollaboratorIcon        string               // config.Theme.Icons.Collaborator
	MemberIcon              string               // config.Theme.Icons.Member
	OwnerIcon               string               // config.Theme.Icons.Owner
	UnknownRoleIcon         string               // config.Theme.Icons.UnknownRole
}

var DefaultTheme = &Theme{
	PrimaryBorder: compat.AdaptiveColor{
		Light: lipgloss.ANSIColor(8),
		Dark:  lipgloss.ANSIColor(8),
	},
	SecondaryBorder: compat.AdaptiveColor{
		Light: lipgloss.ANSIColor(8),
		Dark:  lipgloss.ANSIColor(7),
	},
	SelectedBackground: compat.AdaptiveColor{
		Light: lipgloss.ANSIColor(7),
		Dark:  lipgloss.ANSIColor(236),
	},
	// ActiveBackground bounds the active review comment. Deliberately
	// distinct from SelectedBackground (which marks selected line numbers
	// / list rows): a soft lavender in light terminals, muted indigo in
	// dark, so it reads as "active" without colliding with selection.
	ActiveBackground: compat.AdaptiveColor{
		Light: lipgloss.Color("#EDE7F6"),
		Dark:  lipgloss.Color("#2A2440"),
	},
	// GitHub diff palette (background fills; DiffText is the uniform fg).
	// Light = GitHub light tints; Dark = muted equivalents so the diff
	// card adapts instead of being forced light on a dark canvas.
	DiffAddedBg: compat.AdaptiveColor{
		Light: lipgloss.Color("#DAFBE1"),
		Dark:  lipgloss.Color("#12261E"),
	},
	DiffRemovedBg: compat.AdaptiveColor{
		Light: lipgloss.Color("#FFEBE9"),
		Dark:  lipgloss.Color("#25171C"),
	},
	DiffHeaderBg: compat.AdaptiveColor{
		Light: lipgloss.Color("#DDF4FF"),
		Dark:  lipgloss.Color("#121D2F"),
	},
	DiffContextBg: compat.AdaptiveColor{
		Light: lipgloss.Color("#EAEEF2"),
		Dark:  lipgloss.Color("#161B22"),
	},
	DiffText: compat.AdaptiveColor{
		Light: lipgloss.Color("#1F2328"),
		Dark:  lipgloss.Color("#E6EDF3"),
	},
	FaintBorder: compat.AdaptiveColor{
		Light: lipgloss.ANSIColor(254),
		Dark:  lipgloss.ANSIColor(234),
	},
	PrimaryText: compat.AdaptiveColor{
		Light: lipgloss.ANSIColor(0),
		Dark:  lipgloss.ANSIColor(15),
	},
	SecondaryText: compat.AdaptiveColor{
		Light: lipgloss.ANSIColor(244),
		Dark:  lipgloss.ANSIColor(251),
	},
	FaintText: compat.AdaptiveColor{
		Light: lipgloss.ANSIColor(7),
		Dark:  lipgloss.ANSIColor(245),
	},
	InvertedText: compat.AdaptiveColor{
		Light: lipgloss.ANSIColor(15),
		Dark:  lipgloss.ANSIColor(236),
	},
	SuccessText: compat.AdaptiveColor{
		Light: lipgloss.ANSIColor(10),
		Dark:  lipgloss.ANSIColor(10),
	},
	WarningText: compat.AdaptiveColor{
		Light: lipgloss.ANSIColor(11),
		Dark:  lipgloss.ANSIColor(11),
	},
	ErrorText: compat.AdaptiveColor{
		Light: lipgloss.ANSIColor(1),
		Dark:  lipgloss.ANSIColor(9),
	},
	ActorText: compat.AdaptiveColor{
		Light: lipgloss.ANSIColor(244),
		Dark:  lipgloss.ANSIColor(251),
	}, // Same as SecondaryText
	NewContributorIconColor: compat.AdaptiveColor{
		Light: lipgloss.ANSIColor(77),
		Dark:  lipgloss.ANSIColor(77),
	},
	ContributorIconColor: compat.AdaptiveColor{
		Light: lipgloss.ANSIColor(75),
		Dark:  lipgloss.ANSIColor(75),
	},
	CollaboratorIconColor: compat.AdaptiveColor{
		Light: lipgloss.ANSIColor(178),
		Dark:  lipgloss.ANSIColor(178),
	},
	MemberIconColor: compat.AdaptiveColor{
		Light: lipgloss.ANSIColor(178),
		Dark:  lipgloss.ANSIColor(178),
	},
	OwnerIconColor: compat.AdaptiveColor{
		Light: lipgloss.ANSIColor(178),
		Dark:  lipgloss.ANSIColor(178),
	},
	UnknownRoleIconColor: compat.AdaptiveColor{
		Light: lipgloss.ANSIColor(178),
		Dark:  lipgloss.ANSIColor(178),
	},
	NewContributorIcon: constants.NewContributorIcon,
	ContributorIcon:    constants.ContributorIcon,
	CollaboratorIcon:   constants.CollaboratorIcon,
	MemberIcon:         constants.MemberIcon,
	OwnerIcon:          constants.OwnerIcon,
	UnknownRoleIcon:    constants.UnknownRoleIcon,
}

func ParseTheme(cfg *config.Config) Theme {
	_shimColor := func(color config.Color, fallback compat.AdaptiveColor) compat.AdaptiveColor {
		if color == "" {
			return fallback
		}
		return compat.AdaptiveColor{
			Light: lipgloss.Color(string(color)),
			Dark:  lipgloss.Color(string(color)),
		}
	}
	_shimIcon := func(icon string, fallback string) string {
		if icon != "" {
			return icon
		}
		return fallback
	}

	if cfg.Theme.Colors != nil {
		DefaultTheme.SelectedBackground = _shimColor(
			cfg.Theme.Colors.Inline.Background.Selected,
			DefaultTheme.SelectedBackground,
		)
		DefaultTheme.ActiveBackground = _shimColor(
			cfg.Theme.Colors.Inline.Background.Active,
			DefaultTheme.ActiveBackground,
		)
		DefaultTheme.DiffAddedBg = _shimColor(
			cfg.Theme.Colors.Inline.Diff.Added,
			DefaultTheme.DiffAddedBg,
		)
		DefaultTheme.DiffRemovedBg = _shimColor(
			cfg.Theme.Colors.Inline.Diff.Removed,
			DefaultTheme.DiffRemovedBg,
		)
		DefaultTheme.DiffHeaderBg = _shimColor(
			cfg.Theme.Colors.Inline.Diff.Header,
			DefaultTheme.DiffHeaderBg,
		)
		DefaultTheme.DiffContextBg = _shimColor(
			cfg.Theme.Colors.Inline.Diff.Context,
			DefaultTheme.DiffContextBg,
		)
		DefaultTheme.DiffText = _shimColor(
			cfg.Theme.Colors.Inline.Diff.Text,
			DefaultTheme.DiffText,
		)
		DefaultTheme.PrimaryBorder = _shimColor(
			cfg.Theme.Colors.Inline.Border.Primary,
			DefaultTheme.PrimaryBorder,
		)
		DefaultTheme.FaintBorder = _shimColor(
			cfg.Theme.Colors.Inline.Border.Faint,
			DefaultTheme.FaintBorder,
		)
		DefaultTheme.SecondaryBorder = _shimColor(
			cfg.Theme.Colors.Inline.Border.Secondary,
			DefaultTheme.SecondaryBorder,
		)
		DefaultTheme.FaintText = _shimColor(
			cfg.Theme.Colors.Inline.Text.Faint,
			DefaultTheme.FaintText,
		)
		DefaultTheme.PrimaryText = _shimColor(
			cfg.Theme.Colors.Inline.Text.Primary,
			DefaultTheme.PrimaryText,
		)
		DefaultTheme.SecondaryText = _shimColor(
			cfg.Theme.Colors.Inline.Text.Secondary,
			DefaultTheme.SecondaryText,
		)
		DefaultTheme.InvertedText = _shimColor(
			cfg.Theme.Colors.Inline.Text.Inverted,
			DefaultTheme.InvertedText,
		)
		DefaultTheme.SuccessText = _shimColor(
			cfg.Theme.Colors.Inline.Text.Success,
			DefaultTheme.SuccessText,
		)
		DefaultTheme.WarningText = _shimColor(
			cfg.Theme.Colors.Inline.Text.Warning,
			DefaultTheme.WarningText,
		)
		log.Debug("error text",
			"cfg",
			cfg.Theme.Colors.Inline.Text.Error,
			"default",
			DefaultTheme.ErrorText,
		)
		DefaultTheme.ErrorText = _shimColor(
			cfg.Theme.Colors.Inline.Text.Error,
			DefaultTheme.ErrorText,
		)
		DefaultTheme.ActorText = _shimColor(
			cfg.Theme.Colors.Inline.Text.Actor,
			DefaultTheme.ActorText,
		)
		DefaultTheme.NewContributorIconColor = _shimColor(
			cfg.Theme.Colors.Inline.Icon.NewContributor,
			DefaultTheme.NewContributorIconColor,
		)
		DefaultTheme.ContributorIconColor = _shimColor(
			cfg.Theme.Colors.Inline.Icon.Contributor,
			DefaultTheme.ContributorIconColor,
		)
		DefaultTheme.CollaboratorIconColor = _shimColor(
			cfg.Theme.Colors.Inline.Icon.Collaborator,
			DefaultTheme.CollaboratorIconColor,
		)
		DefaultTheme.MemberIconColor = _shimColor(
			cfg.Theme.Colors.Inline.Icon.Member,
			DefaultTheme.MemberIconColor,
		)
		DefaultTheme.OwnerIconColor = _shimColor(
			cfg.Theme.Colors.Inline.Icon.Owner,
			DefaultTheme.OwnerIconColor,
		)
	}

	if cfg.ShowAuthorIcons && cfg.Theme.Icons != nil {
		DefaultTheme.NewContributorIcon = _shimIcon(
			cfg.Theme.Icons.Inline.NewContributor,
			DefaultTheme.NewContributorIcon,
		)
		DefaultTheme.ContributorIcon = _shimIcon(
			cfg.Theme.Icons.Inline.Contributor,
			DefaultTheme.ContributorIcon,
		)
		DefaultTheme.CollaboratorIcon = _shimIcon(
			cfg.Theme.Icons.Inline.Collaborator,
			DefaultTheme.CollaboratorIcon,
		)
		DefaultTheme.MemberIcon = _shimIcon(
			cfg.Theme.Icons.Inline.Member,
			DefaultTheme.MemberIcon,
		)
		DefaultTheme.OwnerIcon = _shimIcon(
			cfg.Theme.Icons.Inline.Owner,
			DefaultTheme.OwnerIcon,
		)
		DefaultTheme.UnknownRoleIcon = _shimIcon(
			cfg.Theme.Icons.Inline.UnknownRole,
			DefaultTheme.UnknownRoleIcon,
		)
	}

	return *DefaultTheme
}
