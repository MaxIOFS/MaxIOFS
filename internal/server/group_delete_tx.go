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

	if _, err := tx.ExecContext(ctx, `DELETE FROM group_members WHERE group_id = ?`, groupID); err != nil {
		return 0, fmt.Errorf("delete group members: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM bucket_permissions WHERE group_id = ?`, groupID); err != nil {
		return 0, fmt.Errorf("delete group bucket permissions: %w", err)
	}
	result, err := tx.ExecContext(ctx, `DELETE FROM groups WHERE id = ?`, groupID)
	if err != nil {
		return 0, fmt.Errorf("delete group: %w", err)
	}
	if _, err := tx.ExecContext(ctx,
		`DELETE FROM iam_inline_policies WHERE target_type = ? AND target_id = ?`,
		auth.IAMTargetGroup, groupID); err != nil {
		return 0, fmt.Errorf("delete group inline policies: %w", err)
	}
	if _, err := tx.ExecContext(ctx,
		`DELETE FROM iam_policy_attachments WHERE target_type = ? AND target_id = ?`,
		auth.IAMTargetGroup, groupID); err != nil {
		return 0, fmt.Errorf("delete group policy attachments: %w", err)
	}
	if err := cluster.RecordDeletion(ctx, tx, cluster.EntityTypeGroup, groupID, nodeID); err != nil {
		return 0, err
	}
	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("commit group delete: %w", err)
	}

	rowsAffected, _ := result.RowsAffected()
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
