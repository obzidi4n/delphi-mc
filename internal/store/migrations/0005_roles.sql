-- Roles gain developer; bindings were introduced in 0004.

alter table role_bindings drop constraint role_bindings_role_check;
alter table role_bindings add constraint role_bindings_role_check
    check (role in ('owner', 'admin', 'developer', 'viewer'));
