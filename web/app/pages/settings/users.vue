<template>
  <div>
    <div class="page-header">
      <div>
        <h2 class="page-title">Users</h2>
        <p class="page-desc">Manage accounts and access control</p>
      </div>
      <button class="btn btn-primary" disabled>
        <Icon name="user-plus" :size="16" />
        Invite User
      </button>
    </div>

    <!-- Current user -->
    <section class="section">
      <h3 class="section-heading">
        <Icon name="user" :size="14" />
        Your Account
      </h3>
      <div class="profile-card">
        <div class="profile-avatar">
          {{ user?.username?.slice(0, 2).toUpperCase() }}
        </div>
        <div class="profile-info">
          <div class="profile-name">
            {{ user?.username }}
            <Chip v-if="user?.is_admin" gold>Admin</Chip>
          </div>
          <div class="profile-email">
            <Icon name="envelope" :size="12" />
            {{ user?.email }}
          </div>
        </div>
        <div class="profile-actions">
          <button class="btn btn-secondary btn-sm" disabled>
            <Icon name="pencil" :size="13" />
            Edit Profile
          </button>
          <button class="btn btn-secondary btn-sm" disabled>
            <Icon name="key" :size="13" />
            Change Password
          </button>
        </div>
      </div>
    </section>

    <!-- User list -->
    <section class="section">
      <h3 class="section-heading">
        <Icon name="users" :size="14" />
        All Users
      </h3>
      <div class="user-table">
        <div class="user-table-head">
          <span>User</span>
          <span>Role</span>
          <span>Actions</span>
        </div>
        <div class="user-row">
          <div class="user-cell-main">
            <div class="user-avatar-sm">
              {{ user?.username?.slice(0, 2).toUpperCase() }}
            </div>
            <div>
              <div class="user-row-name">{{ user?.username }}</div>
              <div class="user-row-email">{{ user?.email }}</div>
            </div>
          </div>
          <div>
            <span class="role-badge admin" v-if="user?.is_admin">Admin</span>
            <span class="role-badge" v-else>User</span>
          </div>
          <div class="user-row-actions">
            <span class="you-badge">You</span>
          </div>
        </div>
      </div>
      <div class="stub-notice">
        <Icon name="info" :size="14" />
        Multi-user management coming soon — invite links, role assignment, and watch history per user.
      </div>
    </section>

    <!-- Sessions stub -->
    <section class="section">
      <h3 class="section-heading">
        <Icon name="shield" :size="14" />
        Security
      </h3>
      <div class="stub-card">
        <div class="stub-card-icon">
          <Icon name="key" :size="20" />
        </div>
        <div class="stub-card-text">
          <div class="stub-card-title">Active Sessions</div>
          <div class="stub-card-desc">View and manage your active login sessions across devices.</div>
        </div>
        <Chip>Coming Soon</Chip>
      </div>
      <div class="stub-card">
        <div class="stub-card-icon">
          <Icon name="clock" :size="20" />
        </div>
        <div class="stub-card-text">
          <div class="stub-card-title">Login History</div>
          <div class="stub-card-desc">Review recent authentication activity and failed attempts.</div>
        </div>
        <Chip>Coming Soon</Chip>
      </div>
    </section>
  </div>
</template>

<script setup lang="ts">
const { user } = useAuth()
</script>

<style scoped>
.page-header {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  margin-bottom: 28px;
}
.page-title { font-size: 26px; font-weight: 600; letter-spacing: -0.02em; margin: 0; }
.page-desc { font-size: 13px; color: var(--fg-3); margin: 6px 0 0; }

.section { margin-bottom: 36px; }
.section-heading {
  display: flex;
  align-items: center;
  gap: 8px;
  font-size: 11px;
  font-weight: 600;
  color: var(--fg-3);
  font-family: var(--font-mono);
  text-transform: uppercase;
  letter-spacing: 0.1em;
  margin: 0 0 14px;
  padding-bottom: 10px;
  border-bottom: 1px solid var(--border);
}

.btn-sm { height: 34px; padding: 0 14px; font-size: 12px; }

/* Profile card */
.profile-card {
  display: flex;
  align-items: center;
  gap: 18px;
  background: var(--bg-2);
  border: 1px solid var(--border);
  border-radius: var(--r-lg);
  padding: 22px 24px;
}

.profile-avatar {
  width: 52px;
  height: 52px;
  border-radius: 50%;
  background: linear-gradient(135deg, var(--gold-deep), var(--gold));
  color: #1a1408;
  font-size: 15px;
  font-weight: 700;
  display: flex;
  align-items: center;
  justify-content: center;
  letter-spacing: 0.04em;
  flex-shrink: 0;
}

.profile-info { flex: 1; }
.profile-name {
  display: flex;
  align-items: center;
  gap: 8px;
  font-size: 16px;
  font-weight: 600;
}
.profile-email {
  display: flex;
  align-items: center;
  gap: 6px;
  font-size: 13px;
  color: var(--fg-2);
  margin-top: 3px;
}

.profile-actions { display: flex; gap: 6px; flex-shrink: 0; }

/* User table */
.user-table {
  background: var(--bg-2);
  border: 1px solid var(--border);
  border-radius: var(--r-md);
  overflow: hidden;
  margin-bottom: 12px;
}

.user-table-head {
  display: grid;
  grid-template-columns: 1fr 100px 100px;
  gap: 16px;
  padding: 10px 18px;
  font-size: 10px;
  font-weight: 600;
  font-family: var(--font-mono);
  text-transform: uppercase;
  letter-spacing: 0.1em;
  color: var(--fg-3);
  border-bottom: 1px solid var(--border);
}

.user-row {
  display: grid;
  grid-template-columns: 1fr 100px 100px;
  gap: 16px;
  padding: 14px 18px;
  align-items: center;
}

.user-cell-main { display: flex; align-items: center; gap: 12px; }

.user-avatar-sm {
  width: 32px;
  height: 32px;
  border-radius: 50%;
  background: linear-gradient(135deg, var(--gold-deep), var(--gold));
  color: #1a1408;
  font-size: 10px;
  font-weight: 700;
  display: flex;
  align-items: center;
  justify-content: center;
  flex-shrink: 0;
}

.user-row-name { font-size: 13px; font-weight: 500; }
.user-row-email { font-size: 11px; color: var(--fg-3); }

.role-badge {
  font-size: 10px;
  font-weight: 600;
  font-family: var(--font-mono);
  text-transform: uppercase;
  letter-spacing: 0.06em;
  padding: 3px 8px;
  border-radius: 100px;
  background: rgba(255, 255, 255, 0.05);
  color: var(--fg-2);
}

.role-badge.admin {
  background: var(--gold-soft);
  color: var(--gold);
}

.you-badge {
  font-size: 10px;
  font-family: var(--font-mono);
  color: var(--fg-3);
  text-transform: uppercase;
  letter-spacing: 0.06em;
}

.user-row-actions { display: flex; gap: 6px; }

/* Stub cards */
.stub-notice {
  display: flex;
  align-items: center;
  gap: 8px;
  font-size: 12px;
  color: var(--fg-3);
  padding: 10px 14px;
  background: var(--bg-2);
  border: 1px dashed var(--border);
  border-radius: var(--r-md);
}

.stub-card {
  display: flex;
  align-items: center;
  gap: 16px;
  padding: 16px 18px;
  background: var(--bg-2);
  border: 1px solid var(--border);
  border-radius: var(--r-md);
  margin-bottom: 6px;
}

.stub-card-icon {
  width: 40px;
  height: 40px;
  border-radius: var(--r-sm);
  background: rgba(255, 255, 255, 0.04);
  display: flex;
  align-items: center;
  justify-content: center;
  color: var(--fg-3);
  flex-shrink: 0;
}

.stub-card-text { flex: 1; min-width: 0; }
.stub-card-title { font-size: 13px; font-weight: 600; }
.stub-card-desc { font-size: 12px; color: var(--fg-3); margin-top: 2px; }
</style>
