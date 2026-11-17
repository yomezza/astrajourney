package integration

import (
	"context"
	"os"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/yomezza/cosmosimulator/internal/initialization"
	"github.com/yomezza/cosmosimulator/internal/server"
)

var testAssets = server.Assets
var browserAssets = os.DirFS("../../../web")
var graphicAssets = os.DirFS("../../../assets/game")

// Always called with a fixture-owned schema in a disposable _test database.
func openDatabase(ctx context.Context, dsn, schema string) (*pgxpool.Pool, error) {
	return initialization.OpenDatabase(ctx, dsn, schema, testAssets)
}
