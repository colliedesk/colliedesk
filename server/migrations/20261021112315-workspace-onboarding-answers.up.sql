-- What a cloud sign-up said about the team: its first goal, the person's role and where they heard
-- of Colliedesk. One row per workspace created by cloud sign-up; an answer not given is NULL. The
-- goal orders the steps the empty inbox offers first. Older images ignore this table.
CREATE TABLE workspace_onboarding (
    workspace_id uuid PRIMARY KEY REFERENCES workspaces(id) ON DELETE CASCADE,
    goal text CHECK (goal IN ('leave_helpdesk', 'shared_inbox', 'lower_cost', 'ai_agents',
        'other')),
    role text CHECK (role IN ('founder', 'support_lead', 'support_agent', 'developer', 'other')),
    source text CHECK (source IN ('x', 'email', 'search', 'reddit', 'github', 'referral', 'other')),
    created_at timestamptz NOT NULL DEFAULT now()
);

-- Same isolation as saved_views.
SELECT secure_workspace_table('workspace_onboarding');
