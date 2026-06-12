# SSO demo — oauth2-proxy in front of pglockr

A real authenticating reverse proxy (oauth2-proxy) doing OIDC against a test
provider, injecting the user + groups into pglockr, which maps the group to a
role. Proves the `auth.mode: proxy` path end-to-end.

```
browser ──▶ oauth2-proxy :4180 ──▶ pglockr :8111 (proxy/network mode)
                  │
                  └── OIDC ──▶ mock-oauth2-server :8083  (pick email + groups at login)
```

## One-time: /etc/hosts

The test IdP must be reachable at the **same** URL from your browser and from
the oauth2-proxy container. Add:

```
127.0.0.1 host.docker.internal
```

to `/etc/hosts` (Docker Desktop often adds this already — check first).

## Run

```sh
cd ../            && make up      # 1. dev database (:55432)
cd sso            && make up      # 2. oauth2-proxy (:4180) + test IdP (:8083)
make pglockr                       # 3. pglockr in proxy mode (:8111) — leave running
# optional: cd .. && make load-chain   # 4. create a blocking tree to look at
```

Open **http://localhost:4180**. You'll be redirected to the test IdP's login
page — set the **email** and a **claims** JSON granting a pglockr group, e.g.:

```json
{ "groups": ["pglockr-admins"] }
```

Submit, and you're back in pglockr authenticated as that user with the
**admin** role (try `pglockr-ops` → operator, `pglockr-viewers` → viewer; an
operator/admin sees cancel/terminate, a viewer doesn't). No token form appears —
the proxy handled authentication.

## What it demonstrates

- oauth2-proxy guards pglockr: unauthenticated requests get a redirect/401 to
  the IdP (try `curl -i http://localhost:4180/api/me`).
- The group in the OIDC token → `X-Forwarded-Groups` → pglockr role.
- `GET /api/me` reflects the proxy identity; the UI is role-aware.

## Production note

This demo uses `trustMode: network` because oauth2-proxy can't inject a static
secret header, and pglockr's `:8111` is reachable on the host (so don't treat
this as secure — anything on the host could forge the headers). In production
either **network-isolate** pglockr so only the proxy can reach it, or front it
with a proxy that can inject a shared secret (nginx/Istio/Pomerium) and use
`trustMode: secret`.

Teardown: `make down` here and `cd .. && make down`.
