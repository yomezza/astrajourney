package navigation

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/jackc/pgx/v5"

	"github.com/yomezza/cosmosimulator/internal/account"
	"github.com/yomezza/cosmosimulator/internal/commands"
	"github.com/yomezza/cosmosimulator/internal/discovery"
	"github.com/yomezza/cosmosimulator/internal/events"
	"github.com/yomezza/cosmosimulator/internal/fleet"
	"github.com/yomezza/cosmosimulator/internal/universe"
	"github.com/yomezza/cosmosimulator/internal/wallet"
)

func (a *Service) portalCommand(ctx context.Context, tx pgx.Tx, u account.User, s *fleet.Ship, g *universe.Galaxy, in commands.Command) error {
	if err := fleet.Idle(ctx, tx, *s); err != nil {
		return err
	}
	var active bool
	var required, contributed int64
	if err := tx.QueryRow(ctx, `SELECT required,contributed,active FROM portal WHERE id=1 FOR UPDATE`).Scan(&required, &contributed, &active); err != nil {
		return err
	}
	if in.Action == "portal_travel" {
		current := universe.GalaxyKey(g.Stars[s.Star-1])
		targetID := in.Target
		if targetID == "" {
			for _, d := range g.Catalog {
				if d.Opened && d.ID != current {
					targetID = d.ID
					break
				}
			}
		}
		targetDefinition := g.Definition(targetID)
		if targetDefinition == nil || !targetDefinition.Opened || targetID == current {
			return errors.New("Выберите другую открытую галактику")
		}
		near := false
		for _, id := range universe.CoreSystems(g, current) {
			if s.Star == id {
				near = true
			}
		}
		if current == "asterion" && g.Stars[s.Star-1].Number == 1 {
			near = true
		}
		if !near {
			return errors.New("Прибудьте в одну из систем у чёрной дыры")
		}
		cores := universe.CoreSystems(g, targetID)
		if len(cores) == 0 {
			return errors.New("Нет входной системы")
		}
		target := cores[0]
		if targetID == "asterion" {
			for _, star := range g.Stars {
				if universe.GalaxyKey(star) == targetID && star.Number == 1 {
					target = star.ID
					break
				}
			}
		}

		_, err := tx.Exec(ctx, `UPDATE ships SET star=$2,object_id='entry',position_mode='approach',waypoints='[]',reserved_fuel=0 WHERE id=$1`, s.ID, target)
		return err
	}
	if active || in.Amount < 1 || in.Amount > required-contributed {
		return errors.New("Некорректный вклад")
	}
	if err := wallet.Debit(ctx, tx, u.ID, in.Amount); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO portal_contributions(user_id,amount) VALUES($1,$2)`, u.ID, in.Amount); err != nil {
		return err
	}
	_, err := tx.Exec(ctx, `UPDATE portal SET contributed=contributed+$1,active=(contributed+$1>=required) WHERE id=1`, in.Amount)
	if err != nil {
		return err
	}
	if contributed+in.Amount >= required {
		var raw []byte
		if err = tx.QueryRow(ctx, `SELECT data FROM galaxies WHERE name='Edem' FOR UPDATE`).Scan(&raw); err != nil {
			return err
		}
		var expanded universe.Galaxy
		if err = json.Unmarshal(raw, &expanded); err != nil {
			return err
		}
		if !expanded.Definition("asterion").Opened {
			if err = universe.AppendGalaxySystems(&expanded, "asterion", 1000); err != nil {
				return err
			}
			expanded.Regions = append(expanded.Regions, universe.FrontierRegions...)
			universe.AssignGalaxyContent(&expanded, "asterion")
			universe.RebuildGalaxyLanes(&expanded, "asterion", nil)
			expanded.Revision++
			if err = discovery.Sync(ctx, tx, &expanded); err != nil {
				return err
			}
			raw, err = json.Marshal(expanded)
			if err != nil {
				return err
			}
			if _, err = tx.Exec(ctx, `UPDATE galaxies SET data=$1 WHERE name='Edem'`, raw); err != nil {
				return err
			}
		}
		return events.RecordEvent(ctx, tx, u.ID, "portal_open", map[string]any{"galaxy": expanded.Definition("asterion").Name})
	}
	return nil
}
