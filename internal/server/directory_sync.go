package server

import (
	"context"
	"errors"
	"slices"
	"time"

	"github.com/google/uuid"
	"github.com/maxiofs/maxiofs/internal/auth"
	"github.com/maxiofs/maxiofs/internal/idp"
	"github.com/sirupsen/logrus"
)

// directorySyncEvery is how often the directories of the providers with a
// mapping set to sync are read.
const directorySyncEvery = time.Hour

// A user the directory sync deactivated carries this mark in its metadata: it
// is activated again once the directory lets it in. A user an administrator
// deactivated has none, and stays so.
const (
	deactivatedByKey       = "deactivated_by"
	deactivatedByDirectory = "directory"
)

// startDirectorySync reads the directories every directorySyncEvery, on the
// node that runs the cluster's jobs.
func (s *Server) startDirectorySync(ctx context.Context) {
	ticker := time.NewTicker(directorySyncEvery)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
		case <-ctx.Done():
			return
		}
		if s.clusterJobs == nil || s.clusterJobs() {
			s.runTask(ctx, "directory_sync", func() error { s.syncDirectories(ctx); return nil })
		}
	}
}

// syncDirectories reads the directory of each active provider with a mapping
// set to sync: an LDAP directory, or a Keycloak realm. Another OAuth provider
// is asked about a user at the user's sign-in only.
func (s *Server) syncDirectories(ctx context.Context) {
	if s.idpManager == nil {
		return
	}
	providers, err := s.idpManager.ListProviders(ctx, "")
	if err != nil {
		logrus.WithError(err).Warn("Directory sync: failed to list the identity providers")
		return
	}
	for _, provider := range providers {
		if provider.Status != idp.StatusActive || !s.idpManager.ReadsDirectory(ctx, provider.ID) {
			continue
		}
		mappings, err := s.idpManager.ListGroupMappings(ctx, provider.ID)
		if err != nil || !slices.ContainsFunc(mappings, func(m *idp.GroupMapping) bool { return m.AutoSync }) {
			continue
		}
		result := s.syncDirectory(ctx, provider, mappings)
		logrus.WithFields(logrus.Fields{
			"provider":    provider.Name,
			"provider_id": provider.ID,
			"imported":    result.Imported,
			"updated":     result.Updated,
			"deactivated": result.Deactivated,
			"errors":      result.Errors,
		}).Info("Directory synced")
	}
}

// syncDirectory reads provider's directory as a sign-in would, for the users
// that do not sign in: it makes the members of the mappings set to sync that
// have no user, then asks about each of the provider's users. A user the
// directory no longer lets in is deactivated, and activated again once it
// does. A directory that does not answer stops the sync, changing nobody
// further.
func (s *Server) syncDirectory(ctx context.Context, provider *idp.IdentityProvider, mappings []*idp.GroupMapping) idp.SyncResult {
	var result idp.SyncResult
	log := logrus.WithFields(logrus.Fields{"provider": provider.Name, "provider_id": provider.ID})
	for _, mapping := range mappings {
		if !mapping.AutoSync {
			continue
		}
		imported, failed, err := s.provisionMembers(ctx, provider, mapping)
		result.Imported += imported
		result.Errors += failed
		if errors.Is(err, idp.ErrUnavailable) {
			log.WithError(err).Warn("Directory sync stopped: the directory is unavailable")
			result.Errors++
			return result
		}
		if err != nil {
			log.WithError(err).WithField("group", mapping.ExternalGroup).Warn("Directory sync: failed to read a group's members")
			result.Errors++
			continue
		}
		_ = s.idpManager.Store().UpdateGroupMappingSyncTime(mapping.ID)
	}

	users, err := s.authManager.ListUsers(ctx)
	if err != nil {
		log.WithError(err).Warn("Directory sync: failed to list the users")
		result.Errors++
		return result
	}
	bound := authProviderOf(provider)
	for i := range users {
		if users[i].AuthProvider != bound {
			continue
		}
		user, err := s.authManager.GetUser(ctx, users[i].ID)
		if err != nil {
			result.Errors++
			continue
		}
		external, err := s.idpManager.RevalidateSession(ctx, provider.ID, idp.Grant{Subject: user.ExternalID})
		switch {
		case errors.Is(err, idp.ErrUnavailable):
			log.WithError(err).Warn("Directory sync stopped: the directory is unavailable")
			result.Errors++
			return result
		case errors.Is(err, idp.ErrSessionEnded):
			if s.deactivateForDirectory(ctx, user, err.Error()) {
				result.Deactivated++
			}
		case err != nil || external == nil:
			log.WithError(err).WithField("user", user.Username).Warn("Directory sync: failed to ask the directory about a user")
			result.Errors++
		default:
			roles := user.Roles
			user, allowed := s.applyProviderGroups(ctx, user, external)
			if !allowed {
				if s.deactivateForDirectory(ctx, user, "in no group mapped") {
					result.Deactivated++
				}
				continue
			}
			if s.reactivateForDirectory(ctx, user) || !slices.Equal(roles, user.Roles) {
				result.Updated++
			}
		}
	}
	return result
}

// provisionMembers makes the members of mapping's group that have no user,
// following their groups, with the mapping's role: in the provider's tenant,
// or the mapping's for a global provider. It gives how many it made and how
// many it could not, and an error when the members could not be read.
func (s *Server) provisionMembers(ctx context.Context, provider *idp.IdentityProvider, mapping *idp.GroupMapping) (int, int, error) {
	members, err := s.idpManager.GetGroupMembers(ctx, provider.ID, mapping.ExternalGroup)
	if err != nil {
		return 0, 0, err
	}
	bound := authProviderOf(provider)
	// A tenant's provider gives users of its tenant only.
	tenant := provider.TenantID
	if tenant == "" {
		tenant = mapping.TenantID
	}

	imported, failed := 0, 0
	for _, member := range members {
		if existing, _ := s.authManager.FindUserByExternalID(ctx, member.ExternalID, bound); existing != nil {
			continue
		}
		// OAuth providers use email as username for consistency with auto-provisioning
		username := member.Username
		if provider.Type == idp.TypeOAuth2 || username == "" {
			username = member.Email
		}
		if username == "" {
			failed++
			continue
		}
		newUser := &auth.User{
			ID:           "user-" + uuid.New().String()[:8],
			Username:     username,
			DisplayName:  member.DisplayName,
			Email:        member.Email,
			Status:       auth.UserStatusActive,
			TenantID:     tenant,
			Roles:        []string{mapping.Role},
			AuthProvider: bound,
			ExternalID:   member.ExternalID,
			CreatedAt:    time.Now().Unix(),
			UpdatedAt:    time.Now().Unix(),
		}
		markGroupManaged(newUser)
		if err := s.authManager.CreateUser(ctx, newUser); err != nil {
			logrus.WithError(err).WithField("username", username).Warn("Directory sync: failed to make a member's user")
			failed++
			continue
		}
		imported++
	}
	return imported, failed, nil
}

// authProviderOf is how a user of provider names it: ldap:<id> or oauth:<id>.
func authProviderOf(provider *idp.IdentityProvider) string {
	if provider.Type == idp.TypeOAuth2 {
		return "oauth:" + provider.ID
	}
	return provider.Type + ":" + provider.ID
}

// deactivateForDirectory deactivates an active user the directory no longer
// lets in, marked as the directory's doing; it reports whether it did.
func (s *Server) deactivateForDirectory(ctx context.Context, user *auth.User, why string) bool {
	if user.Status != auth.UserStatusActive {
		return false
	}
	user.Status = auth.UserStatusInactive
	if user.Metadata == nil {
		user.Metadata = map[string]string{}
	}
	user.Metadata[deactivatedByKey] = deactivatedByDirectory
	if err := s.authManager.UpdateUser(ctx, user); err != nil {
		logrus.WithError(err).WithField("user", user.Username).Warn("Directory sync: failed to deactivate a user")
		return false
	}
	logrus.WithFields(logrus.Fields{"user": user.Username, "provider": user.AuthProvider, "why": why}).
		Info("Directory sync: user deactivated")
	return true
}

// reactivateForDirectory activates again a user the directory sync
// deactivated; it reports whether it did.
func (s *Server) reactivateForDirectory(ctx context.Context, user *auth.User) bool {
	if user.Status != auth.UserStatusInactive || user.Metadata[deactivatedByKey] != deactivatedByDirectory {
		return false
	}
	user.Status = auth.UserStatusActive
	delete(user.Metadata, deactivatedByKey)
	if err := s.authManager.UpdateUser(ctx, user); err != nil {
		logrus.WithError(err).WithField("user", user.Username).Warn("Directory sync: failed to activate a user again")
		return false
	}
	logrus.WithFields(logrus.Fields{"user": user.Username, "provider": user.AuthProvider}).
		Info("Directory sync: user activated again")
	return true
}
