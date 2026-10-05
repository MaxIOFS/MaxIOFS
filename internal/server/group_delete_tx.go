package server

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/maxiofs/maxiofs/internal/auth"
	"github.com/maxiofs/maxiofs/internal/cluster"
)

func (s *Server) deleteGroupAndRecordTombstone(ctx context.Context, groupID, nodeID string) (int64, error) {
	db := s.groupDeleteDB()
	if db == nil {
		return 0, fmt.Errorf("database is not available")
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck

	groupChanged, _ := cluster.EntityUpdatedAt(ctx, tx, cluster.EntityTypeGroup, groupID)
	policies := iamHeldByTarget(ctx, tx, auth.IAMTargetGroup, groupID)
	rowsAffected, err := deleteGroupRows(ctx, tx, groupID)
	if err != nil {
		return 0, err
	}
	if err := cluster.RecordDeletion(ctx, tx, cluster.EntityTypeGroup, groupID, nodeID, cluster.DeletedAfter(groupChanged)); err != nil {
		return 0, err
	}
	for _, d := range policies {
		if err := cluster.RecordDeletion(ctx, tx, d.entityType, d.id, nodeID, cluster.DeletedAfter(d.lastChange)); err != nil {
			return 0, err
		}
	}
	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("commit group delete: %w", err)
	}
	return rowsAffected, nil
}

func (s *Server) groupDeleteDB() *sql.DB {
	if s.db != nil {
		return s.db
	}
	if s.authManager == nil {
		return nil
	}
	db, _ := s.authManager.GetDB().(*sql.DB)
	return db
}
