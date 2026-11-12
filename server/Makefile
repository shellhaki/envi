.PHONY: secrets deploy deploy-install

# Uploads .env as Worker secrets, leaving out the names wrangler.toml already sets under [vars]:
# Cloudflare refuses a secret and a var with the same name.
secrets:
	@vars=$$(sed -n '/^\[vars\]/,/^\[/p' wrangler.toml | grep -oE '^[A-Z_]+' | paste -sd'|' -); \
	file=$$(mktemp); \
	grep -vE "^($$vars)=" .env > $$file; \
	bunx wrangler secret bulk $$file; status=$$?; \
	rm -f $$file; exit $$status

deploy:
	bunx wrangler deploy

deploy-install:
	bunx wrangler deploy -c install.wrangler.toml
