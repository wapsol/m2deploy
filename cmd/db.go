package cmd

import (
	"os"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"github.com/wapsol/m2deploy/pkg/database"
	"github.com/wapsol/m2deploy/pkg/prereq"
)

var (
	dbBackupPath    string
	dbBackupCompress bool
	dbBackupRetention int
	dbRestoreFile   string
)

var dbCmd = &cobra.Command{
	Use:   "db",
	Short: "Database operations",
	Long:  `Perform database operations like backup, restore, and migrations.`,
}

var dbBackupCmd = &cobra.Command{
	Use:   "backup",
	Short: "Backup the database",
	Long:  `Backup the SQLite database from the backend pod to local storage.`,
	Example: `  m2deploy db backup
  m2deploy db backup --path ./my-backups --compress
  m2deploy db backup --path ./backups --retention 10`,
	RunE: runDBBackup,
}

var dbRestoreCmd = &cobra.Command{
	Use:   "restore",
	Short: "Restore the database",
	Long:  `Restore the SQLite database to the backend pod from a backup file.`,
	Example: `  m2deploy db restore --file ./backups/magnetiq-db-20240101-120000.db.gz
  m2deploy db restore --file ./magnetiq-db.db`,
	RunE: runDBRestore,
}

var dbMigrateCmd = &cobra.Command{
	Use:   "migrate",
	Short: "Run database migrations",
	Long:  `Run database migrations on the backend pod.`,
	Example: `  m2deploy db migrate`,
	RunE: runDBMigrate,
}

var dbStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Check migration status",
	Long:  `Check the current database migration status.`,
	Example: `  m2deploy db status`,
	RunE: runDBStatus,
}

func init() {
	rootCmd.AddCommand(dbCmd)

	// Backup command
	dbCmd.AddCommand(dbBackupCmd)
	dbBackupCmd.Flags().StringVar(&dbBackupPath, "path", "./backups", "Backup directory path")
	dbBackupCmd.Flags().BoolVar(&dbBackupCompress, "compress", true, "Compress backup file")
	dbBackupCmd.Flags().IntVar(&dbBackupRetention, "retention", 5, "Number of backups to keep")

	// Restore command
	dbCmd.AddCommand(dbRestoreCmd)
	dbRestoreCmd.Flags().StringVar(&dbRestoreFile, "file", "", "Backup file to restore")
	dbRestoreCmd.MarkFlagRequired("file")

	// Migrate command
	dbCmd.AddCommand(dbMigrateCmd)

	// Status command
	dbCmd.AddCommand(dbStatusCmd)
}

func runDBBackup(cmd *cobra.Command, args []string) error {
	logger := createLogger()
	defer logger.Close()

	// Always check prerequisites first (fail-fast)
	checker := prereq.NewChecker(logger)
	checker.CheckDBPrereqs(viper.GetString("namespace"), viper.GetBool("use-sudo"))

	// If --check flag is set, print results and exit
	if viper.GetBool("check") {
		checker.PrintResults()
		if checker.HasFailures() {
			os.Exit(1)
		}
		os.Exit(0)
	}

	// Otherwise, fail fast if prerequisites not met
	if checker.HasFailures() {
		checker.PrintResults()
		return formatPrereqError("db")
	}

	dbClient := database.NewClient(
		logger,
		viper.GetBool("dry-run"),
		viper.GetString("namespace"),
		viper.GetString("kubeconfig"),
	)

	if err := dbClient.Backup(dbBackupPath, dbBackupCompress); err != nil {
		return err
	}

	// Clean old backups
	if err := dbClient.CleanOldBackups(dbBackupPath, dbBackupRetention); err != nil {
		logger.Warning("Failed to clean old backups: %v", err)
	}

	return nil
}

func runDBRestore(cmd *cobra.Command, args []string) error {
	logger := createLogger()
	defer logger.Close()

	// Check prerequisites if --check flag is set
	if viper.GetBool("check") {
		checker := prereq.NewChecker(logger)
		checker.CheckDBPrereqs(viper.GetString("namespace"), viper.GetBool("use-sudo"))
		checker.PrintResults()
		if checker.HasFailures() {
			os.Exit(1)
		}
		os.Exit(0)
	}

	dbClient := database.NewClient(
		logger,
		viper.GetBool("dry-run"),
		viper.GetString("namespace"),
		viper.GetString("kubeconfig"),
	)

	logger.Warning("This will overwrite the current database!")
	logger.Info("Make sure to backup the current database first if needed")

	return dbClient.Restore(dbRestoreFile)
}

func runDBMigrate(cmd *cobra.Command, args []string) error {
	logger := createLogger()
	defer logger.Close()

	// Check prerequisites if --check flag is set
	if viper.GetBool("check") {
		checker := prereq.NewChecker(logger)
		checker.CheckDBPrereqs(viper.GetString("namespace"), viper.GetBool("use-sudo"))
		checker.PrintResults()
		if checker.HasFailures() {
			os.Exit(1)
		}
		os.Exit(0)
	}

	dbClient := database.NewClient(
		logger,
		viper.GetBool("dry-run"),
		viper.GetString("namespace"),
		viper.GetString("kubeconfig"),
	)

	return dbClient.Migrate()
}

func runDBStatus(cmd *cobra.Command, args []string) error {
	logger := createLogger()
	defer logger.Close()

	// Check prerequisites if --check flag is set
	if viper.GetBool("check") {
		checker := prereq.NewChecker(logger)
		checker.CheckDBPrereqs(viper.GetString("namespace"), viper.GetBool("use-sudo"))
		checker.PrintResults()
		if checker.HasFailures() {
			os.Exit(1)
		}
		os.Exit(0)
	}

	dbClient := database.NewClient(
		logger,
		viper.GetBool("dry-run"),
		viper.GetString("namespace"),
		viper.GetString("kubeconfig"),
	)

	return dbClient.Status()
}
