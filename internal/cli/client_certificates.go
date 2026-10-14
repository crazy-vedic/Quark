package cli

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/crazy-vedic/quark/internal/config"
)

// NewClientCertificatesCmd manages host-scoped TLS client certificates.
func NewClientCertificatesCmd(configDir string, reload func(config.Config) error) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "cert",
		Short: "Manage host-scoped TLS client certificates",
	}
	cmd.AddCommand(&cobra.Command{
		Use:   "list",
		Short: "List configured client certificates",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg, err := config.Load(configDir)
			if err != nil {
				return fmt.Errorf("cert list: load config: %w", err)
			}
			if len(cfg.HTTP.ClientCertificates) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "No client certificates configured.")
				return nil
			}
			for _, cert := range cfg.HTTP.ClientCertificates {
				fmt.Fprintf(cmd.OutOrStdout(), "%s\t%s\t%s", cert.Host, cert.Type, cert.File)
				if cert.KeyFile != "" {
					fmt.Fprintf(cmd.OutOrStdout(), "\tkey=%s", cert.KeyFile)
				}
				if cert.CAFile != "" {
					fmt.Fprintf(cmd.OutOrStdout(), "\tca=%s", cert.CAFile)
				}
				if cert.PasswordEnv != "" {
					fmt.Fprintf(cmd.OutOrStdout(), "\tpassword-env=%s", cert.PasswordEnv)
				}
				fmt.Fprintln(cmd.OutOrStdout())
			}
			return nil
		},
	})

	var certType, file, keyFile, caFile, passwordEnv string
	setCmd := &cobra.Command{
		Use:   "set <host>",
		Short: "Add or update a host-scoped client certificate",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			host := strings.ToLower(strings.TrimSpace(args[0]))
			file = strings.TrimSpace(file)
			keyFile = strings.TrimSpace(keyFile)
			caFile = strings.TrimSpace(caFile)
			certType = strings.ToUpper(strings.TrimSpace(certType))
			passwordEnv = strings.TrimSpace(passwordEnv)
			if host == "" || (file == "" && caFile == "") {
				return fmt.Errorf("cert set: host and --file or --ca-file are required")
			}
			if certType == "" && file != "" {
				lower := strings.ToLower(file)
				if strings.HasSuffix(lower, ".p12") || strings.HasSuffix(lower, ".pfx") {
					certType = "P12"
				} else {
					certType = "PEM"
				}
			}
			if certType != "" && certType != "P12" && certType != "PEM" {
				return fmt.Errorf("cert set: --type must be P12 or PEM")
			}
			if keyFile != "" && file == "" {
				return fmt.Errorf("cert set: --key-file requires --file")
			}

			cfg, err := config.Load(configDir)
			if err != nil {
				return fmt.Errorf("cert set: load config: %w", err)
			}
			previous := cfg
			next := append([]config.ClientCertificate(nil), cfg.HTTP.ClientCertificates...)
			entry := config.ClientCertificate{
				Host: host, File: file, Type: certType, KeyFile: keyFile,
				CAFile: caFile, PasswordEnv: passwordEnv,
			}
			updated := false
			for index := range next {
				if strings.EqualFold(next[index].Host, host) {
					next[index] = entry
					updated = true
					break
				}
			}
			if !updated {
				next = append(next, entry)
			}
			cfg.HTTP.ClientCertificates = next
			if reload != nil {
				if err := reload(cfg); err != nil {
					return fmt.Errorf("cert set: validate certificate: %w", err)
				}
			}
			if err := config.SaveClientCertificates(configDir, next); err != nil {
				if reload != nil {
					_ = reload(previous)
				}
				return fmt.Errorf("cert set: save config: %w", err)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Saved client certificate for %s\n", host)
			return nil
		},
	}
	setCmd.Flags().StringVar(&file, "file", "", "Client certificate file (PEM, P12, or PFX)")
	setCmd.Flags().StringVar(&certType, "type", "", "Certificate type: PEM or P12 (inferred from file when omitted)")
	setCmd.Flags().StringVar(&keyFile, "key-file", "", "PEM private key file")
	setCmd.Flags().StringVar(&caFile, "ca-file", "", "Optional CA certificate file")
	setCmd.Flags().StringVar(&passwordEnv, "password-env", "", "Environment variable containing the P12 password")
	cmd.AddCommand(setCmd)

	cmd.AddCommand(&cobra.Command{
		Use:   "delete <host>",
		Short: "Remove a configured client certificate",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			host := strings.ToLower(strings.TrimSpace(args[0]))
			cfg, err := config.Load(configDir)
			if err != nil {
				return fmt.Errorf("cert delete: load config: %w", err)
			}
			previous := cfg
			next := make([]config.ClientCertificate, 0, len(cfg.HTTP.ClientCertificates))
			found := false
			for _, cert := range cfg.HTTP.ClientCertificates {
				if strings.EqualFold(cert.Host, host) {
					found = true
					continue
				}
				next = append(next, cert)
			}
			if !found {
				return fmt.Errorf("cert delete: no client certificate configured for %q", host)
			}
			cfg.HTTP.ClientCertificates = next
			if reload != nil {
				if err := reload(cfg); err != nil {
					return fmt.Errorf("cert delete: reload transport: %w", err)
				}
			}
			if err := config.SaveClientCertificates(configDir, next); err != nil {
				if reload != nil {
					_ = reload(previous)
				}
				return fmt.Errorf("cert delete: save config: %w", err)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Deleted client certificate for %s\n", host)
			return nil
		},
	})
	return cmd
}
