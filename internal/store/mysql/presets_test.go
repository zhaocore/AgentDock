package mysql

import (
	"context"
	"database/sql"
	"errors"
	"regexp"
	"strings"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/zhaocore/agentdock/internal/domain"
)

func TestSyncPresetsAtomicUpdateAndRemoval(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	mock.ExpectBegin()
	mock.ExpectExec("UPDATE agents SET enabled = FALSE WHERE owner_user_id IS NULL").WillReturnResult(sqlmock.NewResult(0, 2))
	mock.ExpectQuery("SELECT owner_user_id FROM agents WHERE id = \\? FOR UPDATE").WithArgs("preset-1").WillReturnRows(sqlmock.NewRows([]string{"owner_user_id"}).AddRow(nil))
	mock.ExpectExec("INSERT INTO agents").WithArgs("preset-1", "agent1", "new prompt", "model", []byte(`["review"]`), []byte(`[]`)).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()
	err = SyncPresets(context.Background(), db, []domain.Agent{{ID: "preset-1", Name: "agent1", SystemPrompt: "new prompt", Model: "model", Skills: []string{"review"}, MCPServers: []domain.MCPServerConfig{}}})
	if err != nil {
		t.Fatal(err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestSyncPresetsRollsBackOnFailure(t *testing.T) {
	for _, stage := range []string{"begin", "disable", "owner", "lock", "conflict", "insert", "commit"} {
		t.Run(stage, func(t *testing.T) {
			db, mock, err := sqlmock.New()
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			failure := errors.New("database unavailable")
			agent := domain.Agent{ID: "preset-1", Name: "agent1", Skills: []string{}, MCPServers: []domain.MCPServerConfig{}}
			if stage == "begin" {
				mock.ExpectBegin().WillReturnError(failure)
			} else {
				mock.ExpectBegin()
				if stage == "disable" {
					mock.ExpectExec("UPDATE agents").WillReturnError(failure)
				} else {
					mock.ExpectExec("UPDATE agents").WillReturnResult(sqlmock.NewResult(0, 1))
					if stage == "owner" {
						agent.OwnerUserID = "u1"
					} else {
						lock := mock.ExpectQuery("SELECT owner_user_id").WithArgs(agent.ID)
						switch stage {
						case "lock":
							lock.WillReturnError(failure)
						case "conflict":
							lock.WillReturnRows(sqlmock.NewRows([]string{"owner_user_id"}).AddRow("u1"))
						default:
							lock.WillReturnError(sql.ErrNoRows)
							insert := mock.ExpectExec("INSERT INTO agents")
							if stage == "insert" {
								insert.WillReturnError(failure)
							} else {
								insert.WillReturnResult(sqlmock.NewResult(0, 1))
								mock.ExpectCommit().WillReturnError(failure)
							}
						}
					}
				}
				if stage != "commit" {
					mock.ExpectRollback()
				}
			}
			if err := SyncPresets(context.Background(), db, []domain.Agent{agent}); err == nil {
				t.Fatal("sync must fail")
			}
			if err := mock.ExpectationsWereMet(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestPublicAgentReadStillFiltersPrivateOwner(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	query := `SELECT ` + agentColumns + ` FROM agents WHERE id = ? AND (owner_user_id = ? OR owner_user_id IS NULL) AND enabled = TRUE`
	for _, user := range []string{"u1", "u2"} {
		mock.ExpectQuery(regexp.QuoteMeta(query)).WithArgs("shared", user).WillReturnRows(sqlmock.NewRows(strings.Split(agentColumns, ", ")).AddRow("shared", nil, "agent1", "sp", "model", "[]", "[]", "[]", testTime, testTime))
		agent, err := GetAgent(context.Background(), db, "shared", user)
		if err != nil || agent.OwnerUserID != "" {
			t.Fatalf("public read: %+v %v", agent, err)
		}
	}
	mock.ExpectQuery(regexp.QuoteMeta(query)).WithArgs("private", "other").WillReturnError(sql.ErrNoRows)
	if _, err := GetAgent(context.Background(), db, "private", "other"); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("private read: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
