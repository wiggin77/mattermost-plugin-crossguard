# Integration and smoke test targets for the dual-server Docker dev environment.
# Included from the root Makefile. Relies on variables defined there
# (DOCKER_COMPOSE, MM_HOST, MM_PORT_A, MM_PORT_B, PLUGIN_ID) and the docker-check target.

## End-to-end smoke test: init teams/channels, post message on A, verify relay to B
.PHONY: docker-smoke-test
docker-smoke-test: docker-check
	@echo ""
	@echo "Running end-to-end smoke test..."
	@TOKEN_A=$$(curl -sf -X POST http://$(MM_HOST):$(MM_PORT_A)/api/v4/users/login \
		-d '{"login_id":"admin","password":"password"}' -i 2>/dev/null \
		| grep -i '^Token:' | awk '{print $$2}' | tr -d '\r') && \
	TOKEN_B=$$(curl -sf -X POST http://$(MM_HOST):$(MM_PORT_B)/api/v4/users/login \
		-d '{"login_id":"admin","password":"password"}' -i 2>/dev/null \
		| grep -i '^Token:' | awk '{print $$2}' | tr -d '\r') && \
	echo "Getting team IDs..." && \
	TEAM_A=$$(curl -sf http://$(MM_HOST):$(MM_PORT_A)/api/v4/teams/name/test \
		-H "Authorization: Bearer $$TOKEN_A" | python3 -c "import sys,json; print(json.load(sys.stdin)['id'])") && \
	TEAM_B=$$(curl -sf http://$(MM_HOST):$(MM_PORT_B)/api/v4/teams/name/test \
		-H "Authorization: Bearer $$TOKEN_B" | python3 -c "import sys,json; print(json.load(sys.stdin)['id'])") && \
	echo "Creating dedicated channels..." && \
	LTH_A=$$(curl -sf -X POST http://$(MM_HOST):$(MM_PORT_A)/api/v4/channels \
		-H "Authorization: Bearer $$TOKEN_A" -H "Content-Type: application/json" \
		-d '{"team_id":"'"$$TEAM_A"'","name":"low-to-high","display_name":"Low To High","type":"O"}' \
		2>/dev/null | python3 -c "import sys,json; print(json.load(sys.stdin)['id'])" 2>/dev/null || \
		curl -sf http://$(MM_HOST):$(MM_PORT_A)/api/v4/teams/name/test/channels/name/low-to-high \
		-H "Authorization: Bearer $$TOKEN_A" | python3 -c "import sys,json; print(json.load(sys.stdin)['id'])") && \
	echo "  Server A: low-to-high channel ($$LTH_A)" && \
	LTH_B=$$(curl -sf -X POST http://$(MM_HOST):$(MM_PORT_B)/api/v4/channels \
		-H "Authorization: Bearer $$TOKEN_B" -H "Content-Type: application/json" \
		-d '{"team_id":"'"$$TEAM_B"'","name":"low-to-high","display_name":"Low To High","type":"O"}' \
		2>/dev/null | python3 -c "import sys,json; print(json.load(sys.stdin)['id'])" 2>/dev/null || \
		curl -sf http://$(MM_HOST):$(MM_PORT_B)/api/v4/teams/name/test/channels/name/low-to-high \
		-H "Authorization: Bearer $$TOKEN_B" | python3 -c "import sys,json; print(json.load(sys.stdin)['id'])") && \
	echo "  Server B: low-to-high channel ($$LTH_B)" && \
	BD_A=$$(curl -sf -X POST http://$(MM_HOST):$(MM_PORT_A)/api/v4/channels \
		-H "Authorization: Bearer $$TOKEN_A" -H "Content-Type: application/json" \
		-d '{"team_id":"'"$$TEAM_A"'","name":"bi-directional","display_name":"Bi-Directional","type":"O"}' \
		2>/dev/null | python3 -c "import sys,json; print(json.load(sys.stdin)['id'])" 2>/dev/null || \
		curl -sf http://$(MM_HOST):$(MM_PORT_A)/api/v4/teams/name/test/channels/name/bi-directional \
		-H "Authorization: Bearer $$TOKEN_A" | python3 -c "import sys,json; print(json.load(sys.stdin)['id'])") && \
	echo "  Server A: bi-directional channel ($$BD_A)" && \
	BD_B=$$(curl -sf -X POST http://$(MM_HOST):$(MM_PORT_B)/api/v4/channels \
		-H "Authorization: Bearer $$TOKEN_B" -H "Content-Type: application/json" \
		-d '{"team_id":"'"$$TEAM_B"'","name":"bi-directional","display_name":"Bi-Directional","type":"O"}' \
		2>/dev/null | python3 -c "import sys,json; print(json.load(sys.stdin)['id'])" 2>/dev/null || \
		curl -sf http://$(MM_HOST):$(MM_PORT_B)/api/v4/teams/name/test/channels/name/bi-directional \
		-H "Authorization: Bearer $$TOKEN_B" | python3 -c "import sys,json; print(json.load(sys.stdin)['id'])") && \
	echo "  Server B: bi-directional channel ($$BD_B)" && \
	echo "Adding users to channels..." && \
	USERA_ID=$$(curl -sf http://$(MM_HOST):$(MM_PORT_A)/api/v4/users/username/usera \
		-H "Authorization: Bearer $$TOKEN_A" | python3 -c "import sys,json; print(json.load(sys.stdin)['id'])") && \
	USERAA_ID=$$(curl -sf http://$(MM_HOST):$(MM_PORT_A)/api/v4/users/username/useraa \
		-H "Authorization: Bearer $$TOKEN_A" | python3 -c "import sys,json; print(json.load(sys.stdin)['id'])") && \
	USERB_ID=$$(curl -sf http://$(MM_HOST):$(MM_PORT_B)/api/v4/users/username/userb \
		-H "Authorization: Bearer $$TOKEN_B" | python3 -c "import sys,json; print(json.load(sys.stdin)['id'])") && \
	curl -sf -X POST http://$(MM_HOST):$(MM_PORT_A)/api/v4/channels/$$LTH_A/members \
		-H "Authorization: Bearer $$TOKEN_A" -H "Content-Type: application/json" \
		-d '{"user_id":"'"$$USERA_ID"'"}' >/dev/null 2>&1 || true && \
	curl -sf -X POST http://$(MM_HOST):$(MM_PORT_A)/api/v4/channels/$$BD_A/members \
		-H "Authorization: Bearer $$TOKEN_A" -H "Content-Type: application/json" \
		-d '{"user_id":"'"$$USERA_ID"'"}' >/dev/null 2>&1 || true && \
	echo "  Server A: usera added to low-to-high, bi-directional" && \
	curl -sf -X POST http://$(MM_HOST):$(MM_PORT_A)/api/v4/channels/$$LTH_A/members \
		-H "Authorization: Bearer $$TOKEN_A" -H "Content-Type: application/json" \
		-d '{"user_id":"'"$$USERAA_ID"'"}' >/dev/null 2>&1 || true && \
	curl -sf -X POST http://$(MM_HOST):$(MM_PORT_A)/api/v4/channels/$$BD_A/members \
		-H "Authorization: Bearer $$TOKEN_A" -H "Content-Type: application/json" \
		-d '{"user_id":"'"$$USERAA_ID"'"}' >/dev/null 2>&1 || true && \
	echo "  Server A: useraa added to low-to-high, bi-directional" && \
	curl -sf -X POST http://$(MM_HOST):$(MM_PORT_B)/api/v4/channels/$$LTH_B/members \
		-H "Authorization: Bearer $$TOKEN_B" -H "Content-Type: application/json" \
		-d '{"user_id":"'"$$USERB_ID"'"}' >/dev/null 2>&1 || true && \
	curl -sf -X POST http://$(MM_HOST):$(MM_PORT_B)/api/v4/channels/$$BD_B/members \
		-H "Authorization: Bearer $$TOKEN_B" -H "Content-Type: application/json" \
		-d '{"user_id":"'"$$USERB_ID"'"}' >/dev/null 2>&1 || true && \
	echo "  Server B: userb added to low-to-high, bi-directional" && \
	echo "Initializing teams..." && \
	echo "  Waiting 5s for plugin connections to initialize..." && \
	sleep 5 && \
	INIT_RESP=$$(curl -s -X POST http://$(MM_HOST):$(MM_PORT_A)/api/v4/commands/execute \
		-H "Authorization: Bearer $$TOKEN_A" \
		-H "Content-Type: application/json" \
		-d '{"channel_id":"'"$$LTH_A"'","command":"/crossguard init-team outbound:low-to-high"}') && \
	echo "  Server A: init-team outbound:low-to-high (response: $$INIT_RESP)" && \
	curl -sf -X POST http://$(MM_HOST):$(MM_PORT_A)/api/v4/commands/execute \
		-H "Authorization: Bearer $$TOKEN_A" \
		-H "Content-Type: application/json" \
		-d '{"channel_id":"'"$$LTH_A"'","command":"/crossguard init-team inbound:high-to-low"}' >/dev/null && \
	echo "  Server A: init-team inbound:high-to-low" && \
	curl -sf -X POST http://$(MM_HOST):$(MM_PORT_B)/api/v4/commands/execute \
		-H "Authorization: Bearer $$TOKEN_B" \
		-H "Content-Type: application/json" \
		-d '{"channel_id":"'"$$LTH_B"'","command":"/crossguard init-team inbound:low-to-high"}' >/dev/null && \
	echo "  Server B: init-team inbound:low-to-high" && \
	curl -sf -X POST http://$(MM_HOST):$(MM_PORT_B)/api/v4/commands/execute \
		-H "Authorization: Bearer $$TOKEN_B" \
		-H "Content-Type: application/json" \
		-d '{"channel_id":"'"$$LTH_B"'","command":"/crossguard init-team outbound:high-to-low"}' >/dev/null && \
	echo "  Server B: init-team outbound:high-to-low" && \
	echo "Initializing channels..." && \
	curl -sf -X POST http://$(MM_HOST):$(MM_PORT_A)/api/v4/commands/execute \
		-H "Authorization: Bearer $$TOKEN_A" \
		-H "Content-Type: application/json" \
		-d '{"channel_id":"'"$$LTH_A"'","command":"/crossguard init-channel outbound:low-to-high"}' >/dev/null && \
	echo "  Server A: low-to-high init-channel outbound:low-to-high" && \
	curl -sf -X POST http://$(MM_HOST):$(MM_PORT_B)/api/v4/commands/execute \
		-H "Authorization: Bearer $$TOKEN_B" \
		-H "Content-Type: application/json" \
		-d '{"channel_id":"'"$$LTH_B"'","command":"/crossguard init-channel inbound:low-to-high"}' >/dev/null && \
	echo "  Server B: low-to-high init-channel inbound:low-to-high" && \
	curl -sf -X POST http://$(MM_HOST):$(MM_PORT_A)/api/v4/commands/execute \
		-H "Authorization: Bearer $$TOKEN_A" \
		-H "Content-Type: application/json" \
		-d '{"channel_id":"'"$$BD_A"'","command":"/crossguard init-channel outbound:low-to-high"}' >/dev/null && \
	echo "  Server A: bi-directional init-channel outbound:low-to-high" && \
	curl -sf -X POST http://$(MM_HOST):$(MM_PORT_A)/api/v4/commands/execute \
		-H "Authorization: Bearer $$TOKEN_A" \
		-H "Content-Type: application/json" \
		-d '{"channel_id":"'"$$BD_A"'","command":"/crossguard init-channel inbound:high-to-low"}' >/dev/null && \
	echo "  Server A: bi-directional init-channel inbound:high-to-low" && \
	curl -sf -X POST http://$(MM_HOST):$(MM_PORT_B)/api/v4/commands/execute \
		-H "Authorization: Bearer $$TOKEN_B" \
		-H "Content-Type: application/json" \
		-d '{"channel_id":"'"$$BD_B"'","command":"/crossguard init-channel inbound:low-to-high"}' >/dev/null && \
	echo "  Server B: bi-directional init-channel inbound:low-to-high" && \
	curl -sf -X POST http://$(MM_HOST):$(MM_PORT_B)/api/v4/commands/execute \
		-H "Authorization: Bearer $$TOKEN_B" \
		-H "Content-Type: application/json" \
		-d '{"channel_id":"'"$$BD_B"'","command":"/crossguard init-channel outbound:high-to-low"}' >/dev/null && \
	echo "  Server B: bi-directional init-channel outbound:high-to-low" && \
	echo "Posting smoke-test message from Server A..." && \
	TOKEN_USERA=$$(curl -sf -X POST http://$(MM_HOST):$(MM_PORT_A)/api/v4/users/login \
		-d '{"login_id":"usera","password":"password"}' -i 2>/dev/null \
		| grep -i '^Token:' | awk '{print $$2}' | tr -d '\r') && \
	SMOKE_ID=$$(date +%s)-$$$$ && \
	curl -sf -X POST http://$(MM_HOST):$(MM_PORT_A)/api/v4/posts \
		-H "Authorization: Bearer $$TOKEN_USERA" \
		-H "Content-Type: application/json" \
		-d '{"channel_id":"'"$$LTH_A"'","message":"smoke-test:'"$$SMOKE_ID"'"}' >/dev/null && \
	echo "  Posted smoke-test:$$SMOKE_ID to Server A low-to-high" && \
	echo "Waiting for relay..." && \
	sleep 3 && \
	FOUND=$$(curl -sf "http://$(MM_HOST):$(MM_PORT_B)/api/v4/channels/$$LTH_B/posts?per_page=10" \
		-H "Authorization: Bearer $$TOKEN_B" | python3 -c "import sys,json;data=json.load(sys.stdin);sid='$$SMOKE_ID';found=any('smoke-test:'+sid in p.get('message','') for p in data.get('posts',{}).values());print('PASS' if found else 'FAIL');sys.exit(0 if found else 1)") && \
	echo "Smoke test result: $$FOUND" || \
	{ echo "Smoke test FAILED: message smoke-test:$$SMOKE_ID not found on Server B low-to-high"; exit 1; }

## Full integration test suite (rewrite-team, file relay, XML, Azure)
.PHONY: docker-integration-test
docker-integration-test: docker-check
	@echo ""
	@echo "Running cross-server rewrite-team test..."
	@TOKEN_A=$$(curl -sf -X POST http://$(MM_HOST):$(MM_PORT_A)/api/v4/users/login \
		-d '{"login_id":"admin","password":"password"}' -i 2>/dev/null \
		| grep -i '^Token:' | awk '{print $$2}' | tr -d '\r') && \
	TOKEN_B=$$(curl -sf -X POST http://$(MM_HOST):$(MM_PORT_B)/api/v4/users/login \
		-d '{"login_id":"admin","password":"password"}' -i 2>/dev/null \
		| grep -i '^Token:' | awk '{print $$2}' | tr -d '\r') && \
	echo "Creating rewrite-src team on Server A..." && \
	$(DOCKER_COMPOSE) exec -T mattermost-a mmctl --local team create \
		--name rewrite-src \
		--display-name "Rewrite Source" 2>/dev/null || echo "  Team 'rewrite-src' already exists on Server A" && \
	$(DOCKER_COMPOSE) exec -T mattermost-a mmctl --local team users add rewrite-src admin 2>/dev/null || true && \
	$(DOCKER_COMPOSE) exec -T mattermost-a mmctl --local team users add rewrite-src usera 2>/dev/null || true && \
	echo "Getting team IDs..." && \
	REW_SRC_TEAM=$$(curl -sf http://$(MM_HOST):$(MM_PORT_A)/api/v4/teams/name/rewrite-src \
		-H "Authorization: Bearer $$TOKEN_A" | python3 -c "import sys,json; print(json.load(sys.stdin)['id'])") && \
	TEAM_B=$$(curl -sf http://$(MM_HOST):$(MM_PORT_B)/api/v4/teams/name/test \
		-H "Authorization: Bearer $$TOKEN_B" | python3 -c "import sys,json; print(json.load(sys.stdin)['id'])") && \
	echo "Creating rewrite-test channels..." && \
	REW_A=$$(curl -sf -X POST http://$(MM_HOST):$(MM_PORT_A)/api/v4/channels \
		-H "Authorization: Bearer $$TOKEN_A" -H "Content-Type: application/json" \
		-d '{"team_id":"'"$$REW_SRC_TEAM"'","name":"rewrite-test","display_name":"Rewrite Test","type":"O"}' \
		2>/dev/null | python3 -c "import sys,json; print(json.load(sys.stdin)['id'])" 2>/dev/null || \
		curl -sf http://$(MM_HOST):$(MM_PORT_A)/api/v4/teams/name/rewrite-src/channels/name/rewrite-test \
		-H "Authorization: Bearer $$TOKEN_A" | python3 -c "import sys,json; print(json.load(sys.stdin)['id'])") && \
	echo "  Server A rewrite-src/rewrite-test channel ($$REW_A)" && \
	REW_B=$$(curl -sf -X POST http://$(MM_HOST):$(MM_PORT_B)/api/v4/channels \
		-H "Authorization: Bearer $$TOKEN_B" -H "Content-Type: application/json" \
		-d '{"team_id":"'"$$TEAM_B"'","name":"rewrite-test","display_name":"Rewrite Test","type":"O"}' \
		2>/dev/null | python3 -c "import sys,json; print(json.load(sys.stdin)['id'])" 2>/dev/null || \
		curl -sf http://$(MM_HOST):$(MM_PORT_B)/api/v4/teams/name/test/channels/name/rewrite-test \
		-H "Authorization: Bearer $$TOKEN_B" | python3 -c "import sys,json; print(json.load(sys.stdin)['id'])") && \
	echo "  Server B test/rewrite-test channel ($$REW_B)" && \
	echo "Adding users to channels..." && \
	USERA_ID=$$(curl -sf http://$(MM_HOST):$(MM_PORT_A)/api/v4/users/username/usera \
		-H "Authorization: Bearer $$TOKEN_A" | python3 -c "import sys,json; print(json.load(sys.stdin)['id'])") && \
	USERB_ID=$$(curl -sf http://$(MM_HOST):$(MM_PORT_B)/api/v4/users/username/userb \
		-H "Authorization: Bearer $$TOKEN_B" | python3 -c "import sys,json; print(json.load(sys.stdin)['id'])") && \
	curl -sf -X POST http://$(MM_HOST):$(MM_PORT_A)/api/v4/channels/$$REW_A/members \
		-H "Authorization: Bearer $$TOKEN_A" -H "Content-Type: application/json" \
		-d '{"user_id":"'"$$USERA_ID"'"}' >/dev/null 2>&1 || true && \
	echo "  Server A: usera added to rewrite-src/rewrite-test" && \
	curl -sf -X POST http://$(MM_HOST):$(MM_PORT_B)/api/v4/channels/$$REW_B/members \
		-H "Authorization: Bearer $$TOKEN_B" -H "Content-Type: application/json" \
		-d '{"user_id":"'"$$USERB_ID"'"}' >/dev/null 2>&1 || true && \
	echo "  Server B: userb added to test/rewrite-test" && \
	echo "Initializing teams..." && \
	curl -sf -X POST http://$(MM_HOST):$(MM_PORT_A)/api/v4/commands/execute \
		-H "Authorization: Bearer $$TOKEN_A" \
		-H "Content-Type: application/json" \
		-d '{"channel_id":"'"$$REW_A"'","command":"/crossguard init-team outbound:low-to-high"}' >/dev/null && \
	echo "  Server A rewrite-src: init-team outbound:low-to-high" && \
	curl -sf -X POST http://$(MM_HOST):$(MM_PORT_B)/api/v4/commands/execute \
		-H "Authorization: Bearer $$TOKEN_B" \
		-H "Content-Type: application/json" \
		-d '{"channel_id":"'"$$REW_B"'","command":"/crossguard init-team inbound:low-to-high"}' >/dev/null && \
	echo "  Server B test: init-team inbound:low-to-high (idempotent)" && \
	echo "Initializing channels..." && \
	curl -sf -X POST http://$(MM_HOST):$(MM_PORT_A)/api/v4/commands/execute \
		-H "Authorization: Bearer $$TOKEN_A" \
		-H "Content-Type: application/json" \
		-d '{"channel_id":"'"$$REW_A"'","command":"/crossguard init-channel outbound:low-to-high"}' >/dev/null && \
	echo "  Server A rewrite-src/rewrite-test: init-channel outbound:low-to-high" && \
	curl -sf -X POST http://$(MM_HOST):$(MM_PORT_B)/api/v4/commands/execute \
		-H "Authorization: Bearer $$TOKEN_B" \
		-H "Content-Type: application/json" \
		-d '{"channel_id":"'"$$REW_B"'","command":"/crossguard init-channel inbound:low-to-high"}' >/dev/null && \
	echo "  Server B test/rewrite-test: init-channel inbound:low-to-high" && \
	echo "Setting rewrite-team rule on Server B..." && \
	curl -sf -X POST http://$(MM_HOST):$(MM_PORT_B)/api/v4/commands/execute \
		-H "Authorization: Bearer $$TOKEN_B" \
		-H "Content-Type: application/json" \
		-d '{"channel_id":"'"$$REW_B"'","command":"/crossguard rewrite-team low-to-high rewrite-src"}' >/dev/null && \
	echo "  Server B: rewrite-team low-to-high rewrite-src -> test" && \
	echo "Posting rewrite test message from Server A rewrite-src/rewrite-test..." && \
	TOKEN_USERA=$$(curl -sf -X POST http://$(MM_HOST):$(MM_PORT_A)/api/v4/users/login \
		-d '{"login_id":"usera","password":"password"}' -i 2>/dev/null \
		| grep -i '^Token:' | awk '{print $$2}' | tr -d '\r') && \
	REW_ID=$$(date +%s)-$$$$-rew && \
	curl -sf -X POST http://$(MM_HOST):$(MM_PORT_A)/api/v4/posts \
		-H "Authorization: Bearer $$TOKEN_USERA" \
		-H "Content-Type: application/json" \
		-d '{"channel_id":"'"$$REW_A"'","message":"rewrite-test:'"$$REW_ID"'"}' >/dev/null && \
	echo "  Posted rewrite-test:$$REW_ID to Server A rewrite-src/rewrite-test" && \
	echo "Waiting for relay..." && \
	sleep 3 && \
	REW_FOUND=$$(curl -sf "http://$(MM_HOST):$(MM_PORT_B)/api/v4/channels/$$REW_B/posts?per_page=10" \
		-H "Authorization: Bearer $$TOKEN_B" | python3 -c "import sys,json;data=json.load(sys.stdin);sid='$$REW_ID';found=any('rewrite-test:'+sid in p.get('message','') for p in data.get('posts',{}).values());print('PASS' if found else 'FAIL');sys.exit(0 if found else 1)") && \
	echo "Rewrite-team test result: $$REW_FOUND" || \
	{ echo "Rewrite-team test FAILED: message rewrite-test:$$REW_ID not found on Server B test/rewrite-test"; exit 1; }
	@echo ""
	@echo "Running file attachment relay test..."
	@TOKEN_A=$$(curl -sf -X POST http://$(MM_HOST):$(MM_PORT_A)/api/v4/users/login \
		-d '{"login_id":"admin","password":"password"}' -i 2>/dev/null \
		| grep -i '^Token:' | awk '{print $$2}' | tr -d '\r') && \
	TOKEN_B=$$(curl -sf -X POST http://$(MM_HOST):$(MM_PORT_B)/api/v4/users/login \
		-d '{"login_id":"admin","password":"password"}' -i 2>/dev/null \
		| grep -i '^Token:' | awk '{print $$2}' | tr -d '\r') && \
	TOKEN_USERA=$$(curl -sf -X POST http://$(MM_HOST):$(MM_PORT_A)/api/v4/users/login \
		-d '{"login_id":"usera","password":"password"}' -i 2>/dev/null \
		| grep -i '^Token:' | awk '{print $$2}' | tr -d '\r') && \
	LTH_A=$$(curl -sf http://$(MM_HOST):$(MM_PORT_A)/api/v4/teams/name/test/channels/name/low-to-high \
		-H "Authorization: Bearer $$TOKEN_A" | python3 -c "import sys,json; print(json.load(sys.stdin)['id'])") && \
	LTH_B=$$(curl -sf http://$(MM_HOST):$(MM_PORT_B)/api/v4/teams/name/test/channels/name/low-to-high \
		-H "Authorization: Bearer $$TOKEN_B" | python3 -c "import sys,json; print(json.load(sys.stdin)['id'])") && \
	FILE_ID=$$(date +%s)-$$$$ && \
	echo "Uploading sample.pdf to Server A and posting with message..." && \
	FILE_UPLOAD=$$(curl -sf -X POST "http://$(MM_HOST):$(MM_PORT_A)/api/v4/files?channel_id=$$LTH_A" \
		-H "Authorization: Bearer $$TOKEN_USERA" \
		-F "files=@testdata/sample.pdf" | python3 -c "import sys,json; print(json.load(sys.stdin)['file_infos'][0]['id'])") && \
	POST_RESP=$$(curl -sf -X POST http://$(MM_HOST):$(MM_PORT_A)/api/v4/posts \
		-H "Authorization: Bearer $$TOKEN_USERA" \
		-H "Content-Type: application/json" \
		-d '{"channel_id":"'"$$LTH_A"'","message":"file-test:'"$$FILE_ID"'","file_ids":["'"$$FILE_UPLOAD"'"]}') && \
	echo "  Posted file-test:$$FILE_ID with sample.pdf to Server A low-to-high" && \
	echo "Uploading sample.docx to Server A and posting with message..." && \
	DOCX_ID=$$(date +%s)-$$$$-docx && \
	DOCX_UPLOAD=$$(curl -sf -X POST "http://$(MM_HOST):$(MM_PORT_A)/api/v4/files?channel_id=$$LTH_A" \
		-H "Authorization: Bearer $$TOKEN_USERA" \
		-F "files=@testdata/sample.docx" | python3 -c "import sys,json; print(json.load(sys.stdin)['file_infos'][0]['id'])") && \
	POST_RESP2=$$(curl -sf -X POST http://$(MM_HOST):$(MM_PORT_A)/api/v4/posts \
		-H "Authorization: Bearer $$TOKEN_USERA" \
		-H "Content-Type: application/json" \
		-d '{"channel_id":"'"$$LTH_A"'","message":"file-test:'"$$DOCX_ID"'","file_ids":["'"$$DOCX_UPLOAD"'"]}') && \
	echo "  Posted file-test:$$DOCX_ID with sample.docx to Server A low-to-high" && \
	echo "Waiting for file relay (5s)..." && \
	sleep 5 && \
	echo "Verifying PDF relay on Server B..." && \
	PDF_FOUND=$$(curl -sf "http://$(MM_HOST):$(MM_PORT_B)/api/v4/channels/$$LTH_B/posts?per_page=10" \
		-H "Authorization: Bearer $$TOKEN_B" | python3 -c "import sys,json;data=json.load(sys.stdin);sid='$$FILE_ID';found=any('file-test:'+sid in p.get('message','') and len(p.get('file_ids',[]))>0 for p in data.get('posts',{}).values());print('PASS' if found else 'FAIL');sys.exit(0 if found else 1)") && \
	echo "  PDF file relay test: $$PDF_FOUND" || \
	{ echo "  PDF file relay FAILED: file-test:$$FILE_ID not found with attachments on Server B"; exit 1; } && \
	echo "Verifying DOCX relay on Server B..." && \
	DOCX_FOUND=$$(curl -sf "http://$(MM_HOST):$(MM_PORT_B)/api/v4/channels/$$LTH_B/posts?per_page=10" \
		-H "Authorization: Bearer $$TOKEN_B" | python3 -c "import sys,json;data=json.load(sys.stdin);sid='$$DOCX_ID';found=any('file-test:'+sid in p.get('message','') and len(p.get('file_ids',[]))>0 for p in data.get('posts',{}).values());print('PASS' if found else 'FAIL');sys.exit(0 if found else 1)") && \
	echo "  DOCX file relay test: $$DOCX_FOUND" || \
	{ echo "  DOCX file relay FAILED: file-test:$$DOCX_ID not found with attachments on Server B"; exit 1; }
	@echo ""
	@echo "Running cross-server XML wire format test..."
	@TOKEN_A=$$(curl -sf -X POST http://$(MM_HOST):$(MM_PORT_A)/api/v4/users/login \
		-d '{"login_id":"admin","password":"password"}' -i 2>/dev/null \
		| grep -i '^Token:' | awk '{print $$2}' | tr -d '\r') && \
	TOKEN_B=$$(curl -sf -X POST http://$(MM_HOST):$(MM_PORT_B)/api/v4/users/login \
		-d '{"login_id":"admin","password":"password"}' -i 2>/dev/null \
		| grep -i '^Token:' | awk '{print $$2}' | tr -d '\r') && \
	echo "Creating dedicated XML poster on Server A..." && \
	$(DOCKER_COMPOSE) exec -T mattermost-a mmctl --local user create \
		--email userc@example.com --username userc --password 'password' 2>/dev/null \
		|| echo "  User userc already exists on Server A" && \
	$(DOCKER_COMPOSE) exec -T mattermost-a mmctl --local team users add test userc 2>/dev/null || true && \
	echo "Getting team IDs..." && \
	TEAM_A=$$(curl -sf http://$(MM_HOST):$(MM_PORT_A)/api/v4/teams/name/test \
		-H "Authorization: Bearer $$TOKEN_A" | python3 -c "import sys,json; print(json.load(sys.stdin)['id'])") && \
	TEAM_B=$$(curl -sf http://$(MM_HOST):$(MM_PORT_B)/api/v4/teams/name/test \
		-H "Authorization: Bearer $$TOKEN_B" | python3 -c "import sys,json; print(json.load(sys.stdin)['id'])") && \
	echo "Creating xml-test channels..." && \
	XML_A=$$(curl -sf -X POST http://$(MM_HOST):$(MM_PORT_A)/api/v4/channels \
		-H "Authorization: Bearer $$TOKEN_A" -H "Content-Type: application/json" \
		-d '{"team_id":"'"$$TEAM_A"'","name":"xml-test","display_name":"XML Test","type":"O"}' \
		2>/dev/null | python3 -c "import sys,json; print(json.load(sys.stdin)['id'])" 2>/dev/null || \
		curl -sf http://$(MM_HOST):$(MM_PORT_A)/api/v4/teams/name/test/channels/name/xml-test \
		-H "Authorization: Bearer $$TOKEN_A" | python3 -c "import sys,json; print(json.load(sys.stdin)['id'])") && \
	echo "  Server A test/xml-test channel ($$XML_A)" && \
	XML_B=$$(curl -sf -X POST http://$(MM_HOST):$(MM_PORT_B)/api/v4/channels \
		-H "Authorization: Bearer $$TOKEN_B" -H "Content-Type: application/json" \
		-d '{"team_id":"'"$$TEAM_B"'","name":"xml-test","display_name":"XML Test","type":"O"}' \
		2>/dev/null | python3 -c "import sys,json; print(json.load(sys.stdin)['id'])" 2>/dev/null || \
		curl -sf http://$(MM_HOST):$(MM_PORT_B)/api/v4/teams/name/test/channels/name/xml-test \
		-H "Authorization: Bearer $$TOKEN_B" | python3 -c "import sys,json; print(json.load(sys.stdin)['id'])") && \
	echo "  Server B test/xml-test channel ($$XML_B)" && \
	echo "Adding users to channels..." && \
	USERC_ID=$$(curl -sf http://$(MM_HOST):$(MM_PORT_A)/api/v4/users/username/userc \
		-H "Authorization: Bearer $$TOKEN_A" | python3 -c "import sys,json; print(json.load(sys.stdin)['id'])") && \
	USERB_ID=$$(curl -sf http://$(MM_HOST):$(MM_PORT_B)/api/v4/users/username/userb \
		-H "Authorization: Bearer $$TOKEN_B" | python3 -c "import sys,json; print(json.load(sys.stdin)['id'])") && \
	curl -sf -X POST http://$(MM_HOST):$(MM_PORT_A)/api/v4/channels/$$XML_A/members \
		-H "Authorization: Bearer $$TOKEN_A" -H "Content-Type: application/json" \
		-d '{"user_id":"'"$$USERC_ID"'"}' >/dev/null 2>&1 || true && \
	echo "  Server A: userc added to test/xml-test" && \
	curl -sf -X POST http://$(MM_HOST):$(MM_PORT_B)/api/v4/channels/$$XML_B/members \
		-H "Authorization: Bearer $$TOKEN_B" -H "Content-Type: application/json" \
		-d '{"user_id":"'"$$USERB_ID"'"}' >/dev/null 2>&1 || true && \
	echo "  Server B: userb added to test/xml-test" && \
	echo "Initializing xml-low-to-high team..." && \
	curl -sf -X POST http://$(MM_HOST):$(MM_PORT_A)/api/v4/commands/execute \
		-H "Authorization: Bearer $$TOKEN_A" \
		-H "Content-Type: application/json" \
		-d '{"channel_id":"'"$$XML_A"'","command":"/crossguard init-team outbound:xml-low-to-high"}' >/dev/null && \
	echo "  Server A test: init-team outbound:xml-low-to-high" && \
	curl -sf -X POST http://$(MM_HOST):$(MM_PORT_B)/api/v4/commands/execute \
		-H "Authorization: Bearer $$TOKEN_B" \
		-H "Content-Type: application/json" \
		-d '{"channel_id":"'"$$XML_B"'","command":"/crossguard init-team inbound:xml-low-to-high"}' >/dev/null && \
	echo "  Server B test: init-team inbound:xml-low-to-high" && \
	echo "Initializing xml-test channels..." && \
	curl -sf -X POST http://$(MM_HOST):$(MM_PORT_A)/api/v4/commands/execute \
		-H "Authorization: Bearer $$TOKEN_A" \
		-H "Content-Type: application/json" \
		-d '{"channel_id":"'"$$XML_A"'","command":"/crossguard init-channel outbound:xml-low-to-high"}' >/dev/null && \
	echo "  Server A test/xml-test: init-channel outbound:xml-low-to-high" && \
	curl -sf -X POST http://$(MM_HOST):$(MM_PORT_B)/api/v4/commands/execute \
		-H "Authorization: Bearer $$TOKEN_B" \
		-H "Content-Type: application/json" \
		-d '{"channel_id":"'"$$XML_B"'","command":"/crossguard init-channel inbound:xml-low-to-high"}' >/dev/null && \
	echo "  Server B test/xml-test: init-channel inbound:xml-low-to-high" && \
	echo "Posting XML test message from Server A test/xml-test..." && \
	TOKEN_USERC=$$(curl -sf -X POST http://$(MM_HOST):$(MM_PORT_A)/api/v4/users/login \
		-d '{"login_id":"userc","password":"password"}' -i 2>/dev/null \
		| grep -i '^Token:' | awk '{print $$2}' | tr -d '\r') && \
	XML_ID=$$(date +%s)-$$$$-xml && \
	curl -sf -X POST http://$(MM_HOST):$(MM_PORT_A)/api/v4/posts \
		-H "Authorization: Bearer $$TOKEN_USERC" \
		-H "Content-Type: application/json" \
		-d '{"channel_id":"'"$$XML_A"'","message":"xml-test:'"$$XML_ID"'"}' >/dev/null && \
	echo "  Posted xml-test:$$XML_ID to Server A test/xml-test" && \
	echo "Waiting for XML relay..." && \
	sleep 3 && \
	XML_FOUND=$$(curl -sf "http://$(MM_HOST):$(MM_PORT_B)/api/v4/channels/$$XML_B/posts?per_page=10" \
		-H "Authorization: Bearer $$TOKEN_B" | python3 -c "import sys,json;data=json.load(sys.stdin);sid='$$XML_ID';found=any('xml-test:'+sid in p.get('message','') for p in data.get('posts',{}).values());print('PASS' if found else 'FAIL');sys.exit(0 if found else 1)") && \
	echo "XML test result: $$XML_FOUND" || \
	{ echo "XML test FAILED: message xml-test:$$XML_ID not found on Server B test/xml-test"; exit 1; }
	@echo ""
	@echo "Running post lifecycle test..."
	@$(MAKE) docker-post-lifecycle-test
	@echo ""
	@echo "Running profile image sync test..."
	@$(MAKE) docker-profile-image-test
	@echo ""
	@echo "Running file filter test..."
	@$(MAKE) docker-file-filter-test
	@echo ""
	@echo "Running prompt accept/block test..."
	@$(MAKE) docker-prompt-test
	@echo ""
	@echo "Running Azure integration tests..."
	@$(MAKE) docker-azure-smoke-test
	@$(MAKE) docker-azure-blob-smoke-test
	@$(MAKE) docker-servicebus-smoke-test

## Azure Queue Storage smoke test using Azurite (local emulator).
## Configures a cross-server Azure connection: Server A outbound -> azurite
## queue -> Server B inbound. The plugin's relay path goes through the
## shared-channels framework, which rejects sync messages that try to claim a
## user already owned by another remote, so each provider test uses its own
## dedicated user (userd here) to avoid colliding with the smoke test's
## low-to-high sync of usera.
AZURITE_QUEUE_URL := http://azurite:10001/devstoreaccount1
AZURITE_BLOB_URL := http://azurite:10000/devstoreaccount1
AZURITE_ACCOUNT_NAME := devstoreaccount1
AZURITE_ACCOUNT_KEY := Eby8vdM02xNOcqFlqUwJPLlmEtlCDXJ1OUzFT50uSRZ6IFsuFq2UVErCz4I6tq/K1SZFPTOtr/KBHBeksoGMGw==
AZURITE_QUEUE := crossguard-azure-test
AZURITE_BLOB := crossguard-azure-files
AZURITE_BLOB_BATCH := crossguard-azure-blob-batches

.PHONY: docker-azure-smoke-test
docker-azure-smoke-test: docker-check
	@echo ""
	@echo "Running cross-server Azure Queue smoke test..."
	@$(DOCKER_COMPOSE) exec -T azurite sh -c 'apk add --quiet curl 2>/dev/null; exit 0'
	@echo "Creating dedicated Azure poster (userd) on Server A..."
	@$(DOCKER_COMPOSE) exec -T mattermost-a mmctl --local user create \
		--email userd@example.com --username userd --password 'password' 2>/dev/null \
		|| echo "  User userd already exists on Server A"
	@$(DOCKER_COMPOSE) exec -T mattermost-a mmctl --local team users add test userd 2>/dev/null || true
	@TOKEN_A=$$(curl -sf -X POST http://$(MM_HOST):$(MM_PORT_A)/api/v4/users/login \
		-d '{"login_id":"admin","password":"password"}' -i 2>/dev/null \
		| grep -i '^Token:' | awk '{print $$2}' | tr -d '\r') && \
	TOKEN_B=$$(curl -sf -X POST http://$(MM_HOST):$(MM_PORT_B)/api/v4/users/login \
		-d '{"login_id":"admin","password":"password"}' -i 2>/dev/null \
		| grep -i '^Token:' | awk '{print $$2}' | tr -d '\r') && \
	echo "Getting team IDs..." && \
	TEAM_A=$$(curl -sf http://$(MM_HOST):$(MM_PORT_A)/api/v4/teams/name/test \
		-H "Authorization: Bearer $$TOKEN_A" | python3 -c "import sys,json; print(json.load(sys.stdin)['id'])") && \
	TEAM_B=$$(curl -sf http://$(MM_HOST):$(MM_PORT_B)/api/v4/teams/name/test \
		-H "Authorization: Bearer $$TOKEN_B" | python3 -c "import sys,json; print(json.load(sys.stdin)['id'])") && \
	echo "Creating azure-test channels..." && \
	AZ_A=$$(curl -sf -X POST http://$(MM_HOST):$(MM_PORT_A)/api/v4/channels \
		-H "Authorization: Bearer $$TOKEN_A" -H "Content-Type: application/json" \
		-d '{"team_id":"'"$$TEAM_A"'","name":"azure-test","display_name":"Azure Queue Test","type":"O"}' \
		2>/dev/null | python3 -c "import sys,json; print(json.load(sys.stdin)['id'])" 2>/dev/null || \
		curl -sf http://$(MM_HOST):$(MM_PORT_A)/api/v4/teams/name/test/channels/name/azure-test \
		-H "Authorization: Bearer $$TOKEN_A" | python3 -c "import sys,json; print(json.load(sys.stdin)['id'])") && \
	echo "  Server A test/azure-test channel ($$AZ_A)" && \
	AZ_B=$$(curl -sf -X POST http://$(MM_HOST):$(MM_PORT_B)/api/v4/channels \
		-H "Authorization: Bearer $$TOKEN_B" -H "Content-Type: application/json" \
		-d '{"team_id":"'"$$TEAM_B"'","name":"azure-test","display_name":"Azure Queue Test","type":"O"}' \
		2>/dev/null | python3 -c "import sys,json; print(json.load(sys.stdin)['id'])" 2>/dev/null || \
		curl -sf http://$(MM_HOST):$(MM_PORT_B)/api/v4/teams/name/test/channels/name/azure-test \
		-H "Authorization: Bearer $$TOKEN_B" | python3 -c "import sys,json; print(json.load(sys.stdin)['id'])") && \
	echo "  Server B test/azure-test channel ($$AZ_B)" && \
	echo "Adding users to channels..." && \
	USERD_ID=$$(curl -sf http://$(MM_HOST):$(MM_PORT_A)/api/v4/users/username/userd \
		-H "Authorization: Bearer $$TOKEN_A" | python3 -c "import sys,json; print(json.load(sys.stdin)['id'])") && \
	USERB_ID=$$(curl -sf http://$(MM_HOST):$(MM_PORT_B)/api/v4/users/username/userb \
		-H "Authorization: Bearer $$TOKEN_B" | python3 -c "import sys,json; print(json.load(sys.stdin)['id'])") && \
	curl -sf -X POST http://$(MM_HOST):$(MM_PORT_A)/api/v4/channels/$$AZ_A/members \
		-H "Authorization: Bearer $$TOKEN_A" -H "Content-Type: application/json" \
		-d '{"user_id":"'"$$USERD_ID"'"}' >/dev/null 2>&1 || true && \
	echo "  Server A: userd added to test/azure-test" && \
	curl -sf -X POST http://$(MM_HOST):$(MM_PORT_B)/api/v4/channels/$$AZ_B/members \
		-H "Authorization: Bearer $$TOKEN_B" -H "Content-Type: application/json" \
		-d '{"user_id":"'"$$USERB_ID"'"}' >/dev/null 2>&1 || true && \
	echo "  Server B: userb added to test/azure-test" && \
	echo "Adding azure-low-to-high outbound to Server A config..." && \
	EXISTING_OB_A=$$(curl -sf http://$(MM_HOST):$(MM_PORT_A)/api/v4/config \
		-H "Authorization: Bearer $$TOKEN_A" | python3 -c "import sys,json; c=json.load(sys.stdin); ps=c.get('PluginSettings',{}).get('Plugins',{}).get('crossguard',{}); print(ps.get('outboundconnections','[]'))") && \
	NEW_OB_A=$$(python3 -c "import sys,json; existing=json.loads('$$EXISTING_OB_A'); existing=[c for c in existing if c.get('name')!='azure-low-to-high']; existing.append({\"name\":\"azure-low-to-high\",\"provider\":\"azure-queue\",\"message_format\":\"xml\",\"file_transfer_enabled\":True,\"azure_queue\":{\"queue_service_url\":\"$(AZURITE_QUEUE_URL)\",\"blob_service_url\":\"$(AZURITE_BLOB_URL)\",\"account_name\":\"$(AZURITE_ACCOUNT_NAME)\",\"account_key\":\"$(AZURITE_ACCOUNT_KEY)\",\"queue_name\":\"$(AZURITE_QUEUE)\",\"blob_container_name\":\"$(AZURITE_BLOB)\",\"poll_interval_seconds\":1,\"blob_poll_interval_seconds\":1}}); print(json.dumps(existing))") && \
	curl -sf -X PUT http://$(MM_HOST):$(MM_PORT_A)/api/v4/config/patch \
		-H "Authorization: Bearer $$TOKEN_A" \
		-H "Content-Type: application/json" \
		-d '{"PluginSettings":{"Plugins":{"crossguard":{"outboundconnections":"'"$$(echo $$NEW_OB_A | sed 's/"/\\"/g')"'"}}}}' >/dev/null && \
	echo "  Server A configured with azure-low-to-high outbound" && \
	echo "Adding azure-low-to-high inbound to Server B config..." && \
	EXISTING_IB_B=$$(curl -sf http://$(MM_HOST):$(MM_PORT_B)/api/v4/config \
		-H "Authorization: Bearer $$TOKEN_B" | python3 -c "import sys,json; c=json.load(sys.stdin); ps=c.get('PluginSettings',{}).get('Plugins',{}).get('crossguard',{}); print(ps.get('inboundconnections','[]'))") && \
	NEW_IB_B=$$(python3 -c "import sys,json; existing=json.loads('$$EXISTING_IB_B'); existing=[c for c in existing if c.get('name')!='azure-low-to-high']; existing.append({\"name\":\"azure-low-to-high\",\"provider\":\"azure-queue\",\"message_format\":\"xml\",\"file_transfer_enabled\":True,\"azure_queue\":{\"queue_service_url\":\"$(AZURITE_QUEUE_URL)\",\"blob_service_url\":\"$(AZURITE_BLOB_URL)\",\"account_name\":\"$(AZURITE_ACCOUNT_NAME)\",\"account_key\":\"$(AZURITE_ACCOUNT_KEY)\",\"queue_name\":\"$(AZURITE_QUEUE)\",\"blob_container_name\":\"$(AZURITE_BLOB)\",\"poll_interval_seconds\":1,\"blob_poll_interval_seconds\":1}}); print(json.dumps(existing))") && \
	curl -sf -X PUT http://$(MM_HOST):$(MM_PORT_B)/api/v4/config/patch \
		-H "Authorization: Bearer $$TOKEN_B" \
		-H "Content-Type: application/json" \
		-d '{"PluginSettings":{"Plugins":{"crossguard":{"inboundconnections":"'"$$(echo $$NEW_IB_B | sed 's/"/\\"/g')"'"}}}}' >/dev/null && \
	echo "  Server B configured with azure-low-to-high inbound" && \
	echo "Resetting plugins to pick up new config..." && \
	$(DOCKER_COMPOSE) exec -T mattermost-a mmctl --local plugin disable $(PLUGIN_ID) && \
	$(DOCKER_COMPOSE) exec -T mattermost-a mmctl --local plugin enable $(PLUGIN_ID) && \
	$(DOCKER_COMPOSE) exec -T mattermost-b mmctl --local plugin disable $(PLUGIN_ID) && \
	$(DOCKER_COMPOSE) exec -T mattermost-b mmctl --local plugin enable $(PLUGIN_ID) && \
	sleep 3 && \
	echo "Initializing teams..." && \
	curl -sf -X POST http://$(MM_HOST):$(MM_PORT_A)/api/v4/commands/execute \
		-H "Authorization: Bearer $$TOKEN_A" \
		-H "Content-Type: application/json" \
		-d '{"channel_id":"'"$$AZ_A"'","command":"/crossguard init-team outbound:azure-low-to-high"}' >/dev/null && \
	echo "  Server A test: init-team outbound:azure-low-to-high" && \
	curl -sf -X POST http://$(MM_HOST):$(MM_PORT_B)/api/v4/commands/execute \
		-H "Authorization: Bearer $$TOKEN_B" \
		-H "Content-Type: application/json" \
		-d '{"channel_id":"'"$$AZ_B"'","command":"/crossguard init-team inbound:azure-low-to-high"}' >/dev/null && \
	echo "  Server B test: init-team inbound:azure-low-to-high" && \
	echo "Initializing channels..." && \
	curl -sf -X POST http://$(MM_HOST):$(MM_PORT_A)/api/v4/commands/execute \
		-H "Authorization: Bearer $$TOKEN_A" \
		-H "Content-Type: application/json" \
		-d '{"channel_id":"'"$$AZ_A"'","command":"/crossguard init-channel outbound:azure-low-to-high"}' >/dev/null && \
	echo "  Server A test/azure-test: init-channel outbound:azure-low-to-high" && \
	curl -sf -X POST http://$(MM_HOST):$(MM_PORT_B)/api/v4/commands/execute \
		-H "Authorization: Bearer $$TOKEN_B" \
		-H "Content-Type: application/json" \
		-d '{"channel_id":"'"$$AZ_B"'","command":"/crossguard init-channel inbound:azure-low-to-high"}' >/dev/null && \
	echo "  Server B test/azure-test: init-channel inbound:azure-low-to-high" && \
	echo "Posting Azure smoke-test message from Server A test/azure-test..." && \
	TOKEN_USERD=$$(curl -sf -X POST http://$(MM_HOST):$(MM_PORT_A)/api/v4/users/login \
		-d '{"login_id":"userd","password":"password"}' -i 2>/dev/null \
		| grep -i '^Token:' | awk '{print $$2}' | tr -d '\r') && \
	AZ_ID=$$(date +%s)-$$$$-az && \
	curl -sf -X POST http://$(MM_HOST):$(MM_PORT_A)/api/v4/posts \
		-H "Authorization: Bearer $$TOKEN_USERD" \
		-H "Content-Type: application/json" \
		-d '{"channel_id":"'"$$AZ_A"'","message":"azure-smoke-test:'"$$AZ_ID"'"}' >/dev/null && \
	echo "  Posted azure-smoke-test:$$AZ_ID to Server A test/azure-test" && \
	echo "Polling Server B for relay (up to 20s)..." && \
	AZ_FOUND="FAIL" && \
	for i in 1 2 3 4 5 6 7 8 9 10 11 12 13 14 15 16 17 18 19 20; do \
		sleep 1; \
		AZ_FOUND=$$(curl -sf "http://$(MM_HOST):$(MM_PORT_B)/api/v4/channels/$$AZ_B/posts?per_page=10" \
			-H "Authorization: Bearer $$TOKEN_B" | python3 -c "import sys,json;data=json.load(sys.stdin);sid='$$AZ_ID';found=any('azure-smoke-test:'+sid in p.get('message','') for p in data.get('posts',{}).values());print('PASS' if found else 'FAIL')"); \
		[ "$$AZ_FOUND" = "PASS" ] && break; \
	done && \
	echo "Azure message relay test: $$AZ_FOUND" && \
	[ "$$AZ_FOUND" = "PASS" ] || \
	{ echo "Azure message relay FAILED: azure-smoke-test:$$AZ_ID not found on Server B test/azure-test"; exit 1; }
	@echo ""
	@echo "Running Azure file relay test..."
	@TOKEN_A=$$(curl -sf -X POST http://$(MM_HOST):$(MM_PORT_A)/api/v4/users/login \
		-d '{"login_id":"admin","password":"password"}' -i 2>/dev/null \
		| grep -i '^Token:' | awk '{print $$2}' | tr -d '\r') && \
	TOKEN_B=$$(curl -sf -X POST http://$(MM_HOST):$(MM_PORT_B)/api/v4/users/login \
		-d '{"login_id":"admin","password":"password"}' -i 2>/dev/null \
		| grep -i '^Token:' | awk '{print $$2}' | tr -d '\r') && \
	TOKEN_USERD=$$(curl -sf -X POST http://$(MM_HOST):$(MM_PORT_A)/api/v4/users/login \
		-d '{"login_id":"userd","password":"password"}' -i 2>/dev/null \
		| grep -i '^Token:' | awk '{print $$2}' | tr -d '\r') && \
	AZ_A=$$(curl -sf http://$(MM_HOST):$(MM_PORT_A)/api/v4/teams/name/test/channels/name/azure-test \
		-H "Authorization: Bearer $$TOKEN_A" | python3 -c "import sys,json; print(json.load(sys.stdin)['id'])") && \
	AZ_B=$$(curl -sf http://$(MM_HOST):$(MM_PORT_B)/api/v4/teams/name/test/channels/name/azure-test \
		-H "Authorization: Bearer $$TOKEN_B" | python3 -c "import sys,json; print(json.load(sys.stdin)['id'])") && \
	AZ_FILE_ID=$$(date +%s)-$$$$-azf && \
	echo "Uploading sample.pdf to Server A and posting via Azure connection..." && \
	FILE_UPLOAD=$$(curl -sf -X POST "http://$(MM_HOST):$(MM_PORT_A)/api/v4/files?channel_id=$$AZ_A" \
		-H "Authorization: Bearer $$TOKEN_USERD" \
		-F "files=@testdata/sample.pdf" | python3 -c "import sys,json; print(json.load(sys.stdin)['file_infos'][0]['id'])") && \
	curl -sf -X POST http://$(MM_HOST):$(MM_PORT_A)/api/v4/posts \
		-H "Authorization: Bearer $$TOKEN_USERD" \
		-H "Content-Type: application/json" \
		-d '{"channel_id":"'"$$AZ_A"'","message":"azure-file-test:'"$$AZ_FILE_ID"'","file_ids":["'"$$FILE_UPLOAD"'"]}' >/dev/null && \
	echo "  Posted azure-file-test:$$AZ_FILE_ID with sample.pdf to Server A test/azure-test" && \
	echo "Polling Server B for file relay (up to 20s)..." && \
	AZ_FILE_FOUND="FAIL" && \
	for i in 1 2 3 4 5 6 7 8 9 10 11 12 13 14 15 16 17 18 19 20; do \
		sleep 1; \
		AZ_FILE_FOUND=$$(curl -sf "http://$(MM_HOST):$(MM_PORT_B)/api/v4/channels/$$AZ_B/posts?per_page=10" \
			-H "Authorization: Bearer $$TOKEN_B" | python3 -c "import sys,json;data=json.load(sys.stdin);sid='$$AZ_FILE_ID';found=any('azure-file-test:'+sid in p.get('message','') and len(p.get('file_ids',[]))>0 for p in data.get('posts',{}).values());print('PASS' if found else 'FAIL')"); \
		[ "$$AZ_FILE_FOUND" = "PASS" ] && break; \
	done && \
	echo "Azure file relay test: $$AZ_FILE_FOUND" && \
	[ "$$AZ_FILE_FOUND" = "PASS" ] || \
	{ echo "Azure file relay FAILED: azure-file-test:$$AZ_FILE_ID not found with attachments on Server B test/azure-test"; exit 1; }

## Azure Blob Storage (batched) smoke test using Azurite (local emulator).
## Configures a cross-server azure-blob connection: Server A outbound (WAL
## batch) -> azurite blob -> Server B inbound (poll). Each provider test uses
## its own dedicated user (usere here) to avoid the shared-channels framework
## RemoteID conflict that would otherwise block re-using usera/userd.
.PHONY: docker-azure-blob-smoke-test
docker-azure-blob-smoke-test: docker-check
	@echo ""
	@echo "Running cross-server Azure Blob (batched) smoke test..."
	@echo "Creating dedicated Azure Blob poster (usere) on Server A..."
	@$(DOCKER_COMPOSE) exec -T mattermost-a mmctl --local user create \
		--email usere@example.com --username usere --password 'password' 2>/dev/null \
		|| echo "  User usere already exists on Server A"
	@$(DOCKER_COMPOSE) exec -T mattermost-a mmctl --local team users add test usere 2>/dev/null || true
	@TOKEN_A=$$(curl -sf -X POST http://$(MM_HOST):$(MM_PORT_A)/api/v4/users/login \
		-d '{"login_id":"admin","password":"password"}' -i 2>/dev/null \
		| grep -i '^Token:' | awk '{print $$2}' | tr -d '\r') && \
	TOKEN_B=$$(curl -sf -X POST http://$(MM_HOST):$(MM_PORT_B)/api/v4/users/login \
		-d '{"login_id":"admin","password":"password"}' -i 2>/dev/null \
		| grep -i '^Token:' | awk '{print $$2}' | tr -d '\r') && \
	echo "Getting team IDs..." && \
	TEAM_A=$$(curl -sf http://$(MM_HOST):$(MM_PORT_A)/api/v4/teams/name/test \
		-H "Authorization: Bearer $$TOKEN_A" | python3 -c "import sys,json; print(json.load(sys.stdin)['id'])") && \
	TEAM_B=$$(curl -sf http://$(MM_HOST):$(MM_PORT_B)/api/v4/teams/name/test \
		-H "Authorization: Bearer $$TOKEN_B" | python3 -c "import sys,json; print(json.load(sys.stdin)['id'])") && \
	echo "Creating azure-blob-test channels..." && \
	AZB_A=$$(curl -sf -X POST http://$(MM_HOST):$(MM_PORT_A)/api/v4/channels \
		-H "Authorization: Bearer $$TOKEN_A" -H "Content-Type: application/json" \
		-d '{"team_id":"'"$$TEAM_A"'","name":"azure-blob-test","display_name":"Azure Blob Test","type":"O"}' \
		2>/dev/null | python3 -c "import sys,json; print(json.load(sys.stdin)['id'])" 2>/dev/null || \
		curl -sf http://$(MM_HOST):$(MM_PORT_A)/api/v4/teams/name/test/channels/name/azure-blob-test \
		-H "Authorization: Bearer $$TOKEN_A" | python3 -c "import sys,json; print(json.load(sys.stdin)['id'])") && \
	echo "  Server A test/azure-blob-test channel ($$AZB_A)" && \
	AZB_B=$$(curl -sf -X POST http://$(MM_HOST):$(MM_PORT_B)/api/v4/channels \
		-H "Authorization: Bearer $$TOKEN_B" -H "Content-Type: application/json" \
		-d '{"team_id":"'"$$TEAM_B"'","name":"azure-blob-test","display_name":"Azure Blob Test","type":"O"}' \
		2>/dev/null | python3 -c "import sys,json; print(json.load(sys.stdin)['id'])" 2>/dev/null || \
		curl -sf http://$(MM_HOST):$(MM_PORT_B)/api/v4/teams/name/test/channels/name/azure-blob-test \
		-H "Authorization: Bearer $$TOKEN_B" | python3 -c "import sys,json; print(json.load(sys.stdin)['id'])") && \
	echo "  Server B test/azure-blob-test channel ($$AZB_B)" && \
	echo "Adding users to channels..." && \
	USERE_ID=$$(curl -sf http://$(MM_HOST):$(MM_PORT_A)/api/v4/users/username/usere \
		-H "Authorization: Bearer $$TOKEN_A" | python3 -c "import sys,json; print(json.load(sys.stdin)['id'])") && \
	USERB_ID=$$(curl -sf http://$(MM_HOST):$(MM_PORT_B)/api/v4/users/username/userb \
		-H "Authorization: Bearer $$TOKEN_B" | python3 -c "import sys,json; print(json.load(sys.stdin)['id'])") && \
	curl -sf -X POST http://$(MM_HOST):$(MM_PORT_A)/api/v4/channels/$$AZB_A/members \
		-H "Authorization: Bearer $$TOKEN_A" -H "Content-Type: application/json" \
		-d '{"user_id":"'"$$USERE_ID"'"}' >/dev/null 2>&1 || true && \
	echo "  Server A: usere added to test/azure-blob-test" && \
	curl -sf -X POST http://$(MM_HOST):$(MM_PORT_B)/api/v4/channels/$$AZB_B/members \
		-H "Authorization: Bearer $$TOKEN_B" -H "Content-Type: application/json" \
		-d '{"user_id":"'"$$USERB_ID"'"}' >/dev/null 2>&1 || true && \
	echo "  Server B: userb added to test/azure-blob-test" && \
	echo "Adding azure-blob-low-to-high outbound to Server A config..." && \
	EXISTING_OB_A=$$(curl -sf http://$(MM_HOST):$(MM_PORT_A)/api/v4/config \
		-H "Authorization: Bearer $$TOKEN_A" | python3 -c "import sys,json; c=json.load(sys.stdin); ps=c.get('PluginSettings',{}).get('Plugins',{}).get('crossguard',{}); print(ps.get('outboundconnections','[]'))") && \
	NEW_OB_A=$$(python3 -c "import sys,json; existing=json.loads('$$EXISTING_OB_A'); existing=[c for c in existing if c.get('name')!='azure-blob-low-to-high']; existing.append({\"name\":\"azure-blob-low-to-high\",\"provider\":\"azure-blob\",\"message_format\":\"json\",\"file_transfer_enabled\":True,\"azure_blob\":{\"service_url\":\"$(AZURITE_BLOB_URL)\",\"account_name\":\"$(AZURITE_ACCOUNT_NAME)\",\"account_key\":\"$(AZURITE_ACCOUNT_KEY)\",\"blob_container_name\":\"$(AZURITE_BLOB_BATCH)\",\"flush_interval_seconds\":5,\"batch_poll_interval_seconds\":1}}); print(json.dumps(existing))") && \
	curl -sf -X PUT http://$(MM_HOST):$(MM_PORT_A)/api/v4/config/patch \
		-H "Authorization: Bearer $$TOKEN_A" \
		-H "Content-Type: application/json" \
		-d '{"PluginSettings":{"Plugins":{"crossguard":{"outboundconnections":"'"$$(echo $$NEW_OB_A | sed 's/"/\\"/g')"'"}}}}' >/dev/null && \
	echo "  Server A configured with azure-blob-low-to-high outbound (flush=1s)" && \
	echo "Adding azure-blob-low-to-high inbound to Server B config..." && \
	EXISTING_IB_B=$$(curl -sf http://$(MM_HOST):$(MM_PORT_B)/api/v4/config \
		-H "Authorization: Bearer $$TOKEN_B" | python3 -c "import sys,json; c=json.load(sys.stdin); ps=c.get('PluginSettings',{}).get('Plugins',{}).get('crossguard',{}); print(ps.get('inboundconnections','[]'))") && \
	NEW_IB_B=$$(python3 -c "import sys,json; existing=json.loads('$$EXISTING_IB_B'); existing=[c for c in existing if c.get('name')!='azure-blob-low-to-high']; existing.append({\"name\":\"azure-blob-low-to-high\",\"provider\":\"azure-blob\",\"message_format\":\"json\",\"file_transfer_enabled\":True,\"azure_blob\":{\"service_url\":\"$(AZURITE_BLOB_URL)\",\"account_name\":\"$(AZURITE_ACCOUNT_NAME)\",\"account_key\":\"$(AZURITE_ACCOUNT_KEY)\",\"blob_container_name\":\"$(AZURITE_BLOB_BATCH)\",\"flush_interval_seconds\":5,\"batch_poll_interval_seconds\":1}}); print(json.dumps(existing))") && \
	curl -sf -X PUT http://$(MM_HOST):$(MM_PORT_B)/api/v4/config/patch \
		-H "Authorization: Bearer $$TOKEN_B" \
		-H "Content-Type: application/json" \
		-d '{"PluginSettings":{"Plugins":{"crossguard":{"inboundconnections":"'"$$(echo $$NEW_IB_B | sed 's/"/\\"/g')"'"}}}}' >/dev/null && \
	echo "  Server B configured with azure-blob-low-to-high inbound (poll=1s)" && \
	echo "Resetting plugins to pick up new config..." && \
	$(DOCKER_COMPOSE) exec -T mattermost-a mmctl --local plugin disable $(PLUGIN_ID) && \
	$(DOCKER_COMPOSE) exec -T mattermost-a mmctl --local plugin enable $(PLUGIN_ID) && \
	$(DOCKER_COMPOSE) exec -T mattermost-b mmctl --local plugin disable $(PLUGIN_ID) && \
	$(DOCKER_COMPOSE) exec -T mattermost-b mmctl --local plugin enable $(PLUGIN_ID) && \
	sleep 3 && \
	echo "Initializing teams..." && \
	curl -sf -X POST http://$(MM_HOST):$(MM_PORT_A)/api/v4/commands/execute \
		-H "Authorization: Bearer $$TOKEN_A" \
		-H "Content-Type: application/json" \
		-d '{"channel_id":"'"$$AZB_A"'","command":"/crossguard init-team outbound:azure-blob-low-to-high"}' >/dev/null && \
	echo "  Server A test: init-team outbound:azure-blob-low-to-high" && \
	curl -sf -X POST http://$(MM_HOST):$(MM_PORT_B)/api/v4/commands/execute \
		-H "Authorization: Bearer $$TOKEN_B" \
		-H "Content-Type: application/json" \
		-d '{"channel_id":"'"$$AZB_B"'","command":"/crossguard init-team inbound:azure-blob-low-to-high"}' >/dev/null && \
	echo "  Server B test: init-team inbound:azure-blob-low-to-high" && \
	echo "Initializing channels..." && \
	curl -sf -X POST http://$(MM_HOST):$(MM_PORT_A)/api/v4/commands/execute \
		-H "Authorization: Bearer $$TOKEN_A" \
		-H "Content-Type: application/json" \
		-d '{"channel_id":"'"$$AZB_A"'","command":"/crossguard init-channel outbound:azure-blob-low-to-high"}' >/dev/null && \
	echo "  Server A test/azure-blob-test: init-channel outbound:azure-blob-low-to-high" && \
	curl -sf -X POST http://$(MM_HOST):$(MM_PORT_B)/api/v4/commands/execute \
		-H "Authorization: Bearer $$TOKEN_B" \
		-H "Content-Type: application/json" \
		-d '{"channel_id":"'"$$AZB_B"'","command":"/crossguard init-channel inbound:azure-blob-low-to-high"}' >/dev/null && \
	echo "  Server B test/azure-blob-test: init-channel inbound:azure-blob-low-to-high" && \
	echo "Posting azure-blob smoke-test message from Server A test/azure-blob-test..." && \
	TOKEN_USERE=$$(curl -sf -X POST http://$(MM_HOST):$(MM_PORT_A)/api/v4/users/login \
		-d '{"login_id":"usere","password":"password"}' -i 2>/dev/null \
		| grep -i '^Token:' | awk '{print $$2}' | tr -d '\r') && \
	AZB_ID=$$(date +%s)-$$$$-azb && \
	curl -sf -X POST http://$(MM_HOST):$(MM_PORT_A)/api/v4/posts \
		-H "Authorization: Bearer $$TOKEN_USERE" \
		-H "Content-Type: application/json" \
		-d '{"channel_id":"'"$$AZB_A"'","message":"azure-blob-smoke-test:'"$$AZB_ID"'"}' >/dev/null && \
	echo "  Posted azure-blob-smoke-test:$$AZB_ID to Server A test/azure-blob-test" && \
	echo "Polling Server B for azure-blob relay (up to 30s: 1s flush + 1s poll cadence + warmup)..." && \
	AZB_FOUND="FAIL" && \
	for i in 1 2 3 4 5 6 7 8 9 10 11 12 13 14 15 16 17 18 19 20 21 22 23 24 25 26 27 28 29 30; do \
		sleep 1; \
		AZB_FOUND=$$(curl -sf "http://$(MM_HOST):$(MM_PORT_B)/api/v4/channels/$$AZB_B/posts?per_page=10" \
			-H "Authorization: Bearer $$TOKEN_B" | python3 -c "import sys,json;data=json.load(sys.stdin);sid='$$AZB_ID';found=any('azure-blob-smoke-test:'+sid in p.get('message','') for p in data.get('posts',{}).values());print('PASS' if found else 'FAIL')"); \
		[ "$$AZB_FOUND" = "PASS" ] && break; \
	done && \
	echo "azure-blob message relay test: $$AZB_FOUND" && \
	[ "$$AZB_FOUND" = "PASS" ] || \
	{ echo "azure-blob message relay FAILED: azure-blob-smoke-test:$$AZB_ID not found on Server B test/azure-blob-test"; exit 1; }
	@echo ""
	@echo "Running azure-blob deferred file relay test..."
	@TOKEN_A=$$(curl -sf -X POST http://$(MM_HOST):$(MM_PORT_A)/api/v4/users/login \
		-d '{"login_id":"admin","password":"password"}' -i 2>/dev/null \
		| grep -i '^Token:' | awk '{print $$2}' | tr -d '\r') && \
	TOKEN_B=$$(curl -sf -X POST http://$(MM_HOST):$(MM_PORT_B)/api/v4/users/login \
		-d '{"login_id":"admin","password":"password"}' -i 2>/dev/null \
		| grep -i '^Token:' | awk '{print $$2}' | tr -d '\r') && \
	TOKEN_USERE=$$(curl -sf -X POST http://$(MM_HOST):$(MM_PORT_A)/api/v4/users/login \
		-d '{"login_id":"usere","password":"password"}' -i 2>/dev/null \
		| grep -i '^Token:' | awk '{print $$2}' | tr -d '\r') && \
	AZB_A=$$(curl -sf http://$(MM_HOST):$(MM_PORT_A)/api/v4/teams/name/test/channels/name/azure-blob-test \
		-H "Authorization: Bearer $$TOKEN_A" | python3 -c "import sys,json; print(json.load(sys.stdin)['id'])") && \
	AZB_B=$$(curl -sf http://$(MM_HOST):$(MM_PORT_B)/api/v4/teams/name/test/channels/name/azure-blob-test \
		-H "Authorization: Bearer $$TOKEN_B" | python3 -c "import sys,json; print(json.load(sys.stdin)['id'])") && \
	AZB_FILE_ID=$$(date +%s)-$$$$-azbf && \
	echo "Uploading sample.pdf to Server A and posting via azure-blob connection..." && \
	FILE_UPLOAD=$$(curl -sf -X POST "http://$(MM_HOST):$(MM_PORT_A)/api/v4/files?channel_id=$$AZB_A" \
		-H "Authorization: Bearer $$TOKEN_USERE" \
		-F "files=@testdata/sample.pdf" | python3 -c "import sys,json; print(json.load(sys.stdin)['file_infos'][0]['id'])") && \
	curl -sf -X POST http://$(MM_HOST):$(MM_PORT_A)/api/v4/posts \
		-H "Authorization: Bearer $$TOKEN_USERE" \
		-H "Content-Type: application/json" \
		-d '{"channel_id":"'"$$AZB_A"'","message":"azure-blob-file-test:'"$$AZB_FILE_ID"'","file_ids":["'"$$FILE_UPLOAD"'"]}' >/dev/null && \
	echo "  Posted azure-blob-file-test:$$AZB_FILE_ID with sample.pdf to Server A test/azure-blob-test" && \
	echo "Polling Server B for azure-blob file relay (up to 30s)..." && \
	AZB_FILE_FOUND="FAIL" && \
	for i in 1 2 3 4 5 6 7 8 9 10 11 12 13 14 15 16 17 18 19 20 21 22 23 24 25 26 27 28 29 30; do \
		sleep 1; \
		AZB_FILE_FOUND=$$(curl -sf "http://$(MM_HOST):$(MM_PORT_B)/api/v4/channels/$$AZB_B/posts?per_page=10" \
			-H "Authorization: Bearer $$TOKEN_B" | python3 -c "import sys,json;data=json.load(sys.stdin);sid='$$AZB_FILE_ID';found=any('azure-blob-file-test:'+sid in p.get('message','') and len(p.get('file_ids',[]))>0 for p in data.get('posts',{}).values());print('PASS' if found else 'FAIL')"); \
		[ "$$AZB_FILE_FOUND" = "PASS" ] && break; \
	done && \
	echo "azure-blob deferred file relay test: $$AZB_FILE_FOUND" && \
	[ "$$AZB_FILE_FOUND" = "PASS" ] || \
	{ echo "azure-blob file relay FAILED: azure-blob-file-test:$$AZB_FILE_ID not found with attachments on Server B test/azure-blob-test"; exit 1; }


## Azure Service Bus smoke test using the local Service Bus emulator.
## Readiness is gated by servicebus-probe (AMQP PeekMessages) rather than
## docker healthcheck because the emulator binds 5672 before its SQL schema
## is ready.

SERVICEBUS_PORT_DEFAULT := 5672
SERVICEBUS_QUEUE := crossguard-relay
SERVICEBUS_BLOB := crossguard-servicebus-files
SERVICEBUS_EMULATOR_CONNSTR := Endpoint=sb://servicebus-emulator;SharedAccessKeyName=RootManageSharedAccessKey;SharedAccessKey=SAS_KEY_VALUE;UseDevelopmentEmulator=true
SERVICEBUS_HOST_CONNSTR := Endpoint=sb://localhost:$(SERVICEBUS_PORT_DEFAULT);SharedAccessKeyName=RootManageSharedAccessKey;SharedAccessKey=SAS_KEY_VALUE;UseDevelopmentEmulator=true

.PHONY: servicebus-probe-build
servicebus-probe-build:
	@echo "Building servicebus-probe..."
	@go build -o ./build/bin/servicebus-probe ./build/servicebus-probe

## Bring up the Service Bus emulator + SQL sidecar (servicebus compose profile).
## The servicebus-* services are profiled so a plain `docker compose up` skips
## them; this target enables the profile explicitly. Idempotent.
.PHONY: docker-servicebus-up
docker-servicebus-up:
	@echo "Starting Service Bus emulator and SQL sidecar (profile: servicebus)..."
	@$(DOCKER_COMPOSE) --profile servicebus up -d servicebus-emulator

.PHONY: servicebus-probe-run
servicebus-probe-run: servicebus-probe-build docker-servicebus-up
	@echo "Waiting for Service Bus emulator to become ready..."
	@./build/bin/servicebus-probe -connstr '$(SERVICEBUS_HOST_CONNSTR)' -queue '$(SERVICEBUS_QUEUE)' -deadline 120s

.PHONY: docker-servicebus-smoke-test
docker-servicebus-smoke-test: docker-check servicebus-probe-run
	@echo ""
	@echo "Running cross-server Azure Service Bus smoke test..."
	@echo "Creating dedicated Service Bus poster (userf) on Server A..."
	@$(DOCKER_COMPOSE) exec -T mattermost-a mmctl --local user create \
		--email userf@example.com --username userf --password 'password' 2>/dev/null \
		|| echo "  User userf already exists on Server A"
	@$(DOCKER_COMPOSE) exec -T mattermost-a mmctl --local team users add test userf 2>/dev/null || true
	@TOKEN_A=$$(curl -sf -X POST http://$(MM_HOST):$(MM_PORT_A)/api/v4/users/login \
		-d '{"login_id":"admin","password":"password"}' -i 2>/dev/null \
		| grep -i '^Token:' | awk '{print $$2}' | tr -d '\r') && \
	TOKEN_B=$$(curl -sf -X POST http://$(MM_HOST):$(MM_PORT_B)/api/v4/users/login \
		-d '{"login_id":"admin","password":"password"}' -i 2>/dev/null \
		| grep -i '^Token:' | awk '{print $$2}' | tr -d '\r') && \
	echo "Getting team IDs..." && \
	TEAM_A=$$(curl -sf http://$(MM_HOST):$(MM_PORT_A)/api/v4/teams/name/test \
		-H "Authorization: Bearer $$TOKEN_A" | python3 -c "import sys,json; print(json.load(sys.stdin)['id'])") && \
	TEAM_B=$$(curl -sf http://$(MM_HOST):$(MM_PORT_B)/api/v4/teams/name/test \
		-H "Authorization: Bearer $$TOKEN_B" | python3 -c "import sys,json; print(json.load(sys.stdin)['id'])") && \
	echo "Creating servicebus-test channels..." && \
	SB_A=$$(curl -sf -X POST http://$(MM_HOST):$(MM_PORT_A)/api/v4/channels \
		-H "Authorization: Bearer $$TOKEN_A" -H "Content-Type: application/json" \
		-d '{"team_id":"'"$$TEAM_A"'","name":"servicebus-test","display_name":"Service Bus Test","type":"O"}' \
		2>/dev/null | python3 -c "import sys,json; print(json.load(sys.stdin)['id'])" 2>/dev/null || \
		curl -sf http://$(MM_HOST):$(MM_PORT_A)/api/v4/teams/name/test/channels/name/servicebus-test \
		-H "Authorization: Bearer $$TOKEN_A" | python3 -c "import sys,json; print(json.load(sys.stdin)['id'])") && \
	echo "  Server A test/servicebus-test channel ($$SB_A)" && \
	SB_B=$$(curl -sf -X POST http://$(MM_HOST):$(MM_PORT_B)/api/v4/channels \
		-H "Authorization: Bearer $$TOKEN_B" -H "Content-Type: application/json" \
		-d '{"team_id":"'"$$TEAM_B"'","name":"servicebus-test","display_name":"Service Bus Test","type":"O"}' \
		2>/dev/null | python3 -c "import sys,json; print(json.load(sys.stdin)['id'])" 2>/dev/null || \
		curl -sf http://$(MM_HOST):$(MM_PORT_B)/api/v4/teams/name/test/channels/name/servicebus-test \
		-H "Authorization: Bearer $$TOKEN_B" | python3 -c "import sys,json; print(json.load(sys.stdin)['id'])") && \
	echo "  Server B test/servicebus-test channel ($$SB_B)" && \
	echo "Adding users to channels..." && \
	USERF_ID=$$(curl -sf http://$(MM_HOST):$(MM_PORT_A)/api/v4/users/username/userf \
		-H "Authorization: Bearer $$TOKEN_A" | python3 -c "import sys,json; print(json.load(sys.stdin)['id'])") && \
	USERB_ID=$$(curl -sf http://$(MM_HOST):$(MM_PORT_B)/api/v4/users/username/userb \
		-H "Authorization: Bearer $$TOKEN_B" | python3 -c "import sys,json; print(json.load(sys.stdin)['id'])") && \
	curl -sf -X POST http://$(MM_HOST):$(MM_PORT_A)/api/v4/channels/$$SB_A/members \
		-H "Authorization: Bearer $$TOKEN_A" -H "Content-Type: application/json" \
		-d '{"user_id":"'"$$USERF_ID"'"}' >/dev/null 2>&1 || true && \
	echo "  Server A: userf added to test/servicebus-test" && \
	curl -sf -X POST http://$(MM_HOST):$(MM_PORT_B)/api/v4/channels/$$SB_B/members \
		-H "Authorization: Bearer $$TOKEN_B" -H "Content-Type: application/json" \
		-d '{"user_id":"'"$$USERB_ID"'"}' >/dev/null 2>&1 || true && \
	echo "  Server B: userb added to test/servicebus-test" && \
	echo "Adding servicebus-low-to-high outbound to Server A config..." && \
	EXISTING_OB_A=$$(curl -sf http://$(MM_HOST):$(MM_PORT_A)/api/v4/config \
		-H "Authorization: Bearer $$TOKEN_A" | python3 -c "import sys,json; c=json.load(sys.stdin); ps=c.get('PluginSettings',{}).get('Plugins',{}).get('crossguard',{}); print(ps.get('outboundconnections','[]'))") && \
	NEW_OB_A=$$(python3 -c "import sys,json; existing=json.loads('$$EXISTING_OB_A'); existing=[c for c in existing if c.get('name')!='servicebus-low-to-high']; existing.append({\"name\":\"servicebus-low-to-high\",\"provider\":\"azure-servicebus\",\"message_format\":\"xml\",\"file_transfer_enabled\":True,\"azure_servicebus\":{\"connection_string\":\"$(SERVICEBUS_EMULATOR_CONNSTR)\",\"queue_name\":\"$(SERVICEBUS_QUEUE)\",\"blob_service_url\":\"$(AZURITE_BLOB_URL)\",\"blob_account_name\":\"$(AZURITE_ACCOUNT_NAME)\",\"blob_account_key\":\"$(AZURITE_ACCOUNT_KEY)\",\"blob_container_name\":\"$(SERVICEBUS_BLOB)\",\"blob_poll_interval_seconds\":1}}); print(json.dumps(existing))") && \
	curl -sf -X PUT http://$(MM_HOST):$(MM_PORT_A)/api/v4/config/patch \
		-H "Authorization: Bearer $$TOKEN_A" \
		-H "Content-Type: application/json" \
		-d '{"PluginSettings":{"Plugins":{"crossguard":{"outboundconnections":"'"$$(echo $$NEW_OB_A | sed 's/"/\\"/g')"'"}}}}' >/dev/null && \
	echo "  Server A configured with servicebus-low-to-high outbound" && \
	echo "Adding servicebus-low-to-high inbound to Server B config..." && \
	EXISTING_IB_B=$$(curl -sf http://$(MM_HOST):$(MM_PORT_B)/api/v4/config \
		-H "Authorization: Bearer $$TOKEN_B" | python3 -c "import sys,json; c=json.load(sys.stdin); ps=c.get('PluginSettings',{}).get('Plugins',{}).get('crossguard',{}); print(ps.get('inboundconnections','[]'))") && \
	NEW_IB_B=$$(python3 -c "import sys,json; existing=json.loads('$$EXISTING_IB_B'); existing=[c for c in existing if c.get('name')!='servicebus-low-to-high']; existing.append({\"name\":\"servicebus-low-to-high\",\"provider\":\"azure-servicebus\",\"message_format\":\"xml\",\"file_transfer_enabled\":True,\"azure_servicebus\":{\"connection_string\":\"$(SERVICEBUS_EMULATOR_CONNSTR)\",\"queue_name\":\"$(SERVICEBUS_QUEUE)\",\"blob_service_url\":\"$(AZURITE_BLOB_URL)\",\"blob_account_name\":\"$(AZURITE_ACCOUNT_NAME)\",\"blob_account_key\":\"$(AZURITE_ACCOUNT_KEY)\",\"blob_container_name\":\"$(SERVICEBUS_BLOB)\",\"blob_poll_interval_seconds\":1}}); print(json.dumps(existing))") && \
	curl -sf -X PUT http://$(MM_HOST):$(MM_PORT_B)/api/v4/config/patch \
		-H "Authorization: Bearer $$TOKEN_B" \
		-H "Content-Type: application/json" \
		-d '{"PluginSettings":{"Plugins":{"crossguard":{"inboundconnections":"'"$$(echo $$NEW_IB_B | sed 's/"/\\"/g')"'"}}}}' >/dev/null && \
	echo "  Server B configured with servicebus-low-to-high inbound" && \
	echo "Resetting plugins to pick up new config..." && \
	$(DOCKER_COMPOSE) exec -T mattermost-a mmctl --local plugin disable $(PLUGIN_ID) && \
	$(DOCKER_COMPOSE) exec -T mattermost-a mmctl --local plugin enable $(PLUGIN_ID) && \
	$(DOCKER_COMPOSE) exec -T mattermost-b mmctl --local plugin disable $(PLUGIN_ID) && \
	$(DOCKER_COMPOSE) exec -T mattermost-b mmctl --local plugin enable $(PLUGIN_ID) && \
	sleep 3 && \
	echo "Initializing teams..." && \
	curl -sf -X POST http://$(MM_HOST):$(MM_PORT_A)/api/v4/commands/execute \
		-H "Authorization: Bearer $$TOKEN_A" \
		-H "Content-Type: application/json" \
		-d '{"channel_id":"'"$$SB_A"'","command":"/crossguard init-team outbound:servicebus-low-to-high"}' >/dev/null && \
	echo "  Server A test: init-team outbound:servicebus-low-to-high" && \
	curl -sf -X POST http://$(MM_HOST):$(MM_PORT_B)/api/v4/commands/execute \
		-H "Authorization: Bearer $$TOKEN_B" \
		-H "Content-Type: application/json" \
		-d '{"channel_id":"'"$$SB_B"'","command":"/crossguard init-team inbound:servicebus-low-to-high"}' >/dev/null && \
	echo "  Server B test: init-team inbound:servicebus-low-to-high" && \
	echo "Initializing channels..." && \
	curl -sf -X POST http://$(MM_HOST):$(MM_PORT_A)/api/v4/commands/execute \
		-H "Authorization: Bearer $$TOKEN_A" \
		-H "Content-Type: application/json" \
		-d '{"channel_id":"'"$$SB_A"'","command":"/crossguard init-channel outbound:servicebus-low-to-high"}' >/dev/null && \
	echo "  Server A test/servicebus-test: init-channel outbound:servicebus-low-to-high" && \
	curl -sf -X POST http://$(MM_HOST):$(MM_PORT_B)/api/v4/commands/execute \
		-H "Authorization: Bearer $$TOKEN_B" \
		-H "Content-Type: application/json" \
		-d '{"channel_id":"'"$$SB_B"'","command":"/crossguard init-channel inbound:servicebus-low-to-high"}' >/dev/null && \
	echo "  Server B test/servicebus-test: init-channel inbound:servicebus-low-to-high" && \
	echo "Posting Service Bus smoke-test message from Server A test/servicebus-test..." && \
	TOKEN_USERF=$$(curl -sf -X POST http://$(MM_HOST):$(MM_PORT_A)/api/v4/users/login \
		-d '{"login_id":"userf","password":"password"}' -i 2>/dev/null \
		| grep -i '^Token:' | awk '{print $$2}' | tr -d '\r') && \
	SB_ID=$$(date +%s)-$$$$-sb && \
	curl -sf -X POST http://$(MM_HOST):$(MM_PORT_A)/api/v4/posts \
		-H "Authorization: Bearer $$TOKEN_USERF" \
		-H "Content-Type: application/json" \
		-d '{"channel_id":"'"$$SB_A"'","message":"servicebus-smoke-test:'"$$SB_ID"'"}' >/dev/null && \
	echo "  Posted servicebus-smoke-test:$$SB_ID to Server A test/servicebus-test" && \
	echo "Polling Server B for Service Bus relay (up to 60s; first-init relay takes longer)..." && \
	SB_FOUND="FAIL" && \
	for i in $$(seq 1 60); do \
		sleep 1; \
		SB_FOUND=$$(curl -sf "http://$(MM_HOST):$(MM_PORT_B)/api/v4/channels/$$SB_B/posts?per_page=10" \
			-H "Authorization: Bearer $$TOKEN_B" | python3 -c "import sys,json;data=json.load(sys.stdin);sid='$$SB_ID';found=any('servicebus-smoke-test:'+sid in p.get('message','') for p in data.get('posts',{}).values());print('PASS' if found else 'FAIL')"); \
		[ "$$SB_FOUND" = "PASS" ] && break; \
	done && \
	echo "Service Bus message relay test: $$SB_FOUND" && \
	[ "$$SB_FOUND" = "PASS" ] || \
	{ echo "Service Bus message relay FAILED: servicebus-smoke-test:$$SB_ID not found on Server B test/servicebus-test"; exit 1; }
	@echo ""
	@echo "Running Service Bus file relay test..."
	@TOKEN_A=$$(curl -sf -X POST http://$(MM_HOST):$(MM_PORT_A)/api/v4/users/login \
		-d '{"login_id":"admin","password":"password"}' -i 2>/dev/null \
		| grep -i '^Token:' | awk '{print $$2}' | tr -d '\r') && \
	TOKEN_B=$$(curl -sf -X POST http://$(MM_HOST):$(MM_PORT_B)/api/v4/users/login \
		-d '{"login_id":"admin","password":"password"}' -i 2>/dev/null \
		| grep -i '^Token:' | awk '{print $$2}' | tr -d '\r') && \
	TOKEN_USERF=$$(curl -sf -X POST http://$(MM_HOST):$(MM_PORT_A)/api/v4/users/login \
		-d '{"login_id":"userf","password":"password"}' -i 2>/dev/null \
		| grep -i '^Token:' | awk '{print $$2}' | tr -d '\r') && \
	SB_A=$$(curl -sf http://$(MM_HOST):$(MM_PORT_A)/api/v4/teams/name/test/channels/name/servicebus-test \
		-H "Authorization: Bearer $$TOKEN_A" | python3 -c "import sys,json; print(json.load(sys.stdin)['id'])") && \
	SB_B=$$(curl -sf http://$(MM_HOST):$(MM_PORT_B)/api/v4/teams/name/test/channels/name/servicebus-test \
		-H "Authorization: Bearer $$TOKEN_B" | python3 -c "import sys,json; print(json.load(sys.stdin)['id'])") && \
	SB_FILE_ID=$$(date +%s)-$$$$-sbf && \
	echo "Uploading sample.pdf to Server A and posting via Service Bus connection..." && \
	FILE_UPLOAD=$$(curl -sf -X POST "http://$(MM_HOST):$(MM_PORT_A)/api/v4/files?channel_id=$$SB_A" \
		-H "Authorization: Bearer $$TOKEN_USERF" -F "files=@testdata/sample.pdf" \
		| python3 -c "import sys,json; print(json.load(sys.stdin)['file_infos'][0]['id'])") && \
	curl -sf -X POST http://$(MM_HOST):$(MM_PORT_A)/api/v4/posts \
		-H "Authorization: Bearer $$TOKEN_USERF" -H "Content-Type: application/json" \
		-d '{"channel_id":"'"$$SB_A"'","message":"servicebus-file-test:'"$$SB_FILE_ID"'","file_ids":["'"$$FILE_UPLOAD"'"]}' >/dev/null && \
	echo "  Posted servicebus-file-test:$$SB_FILE_ID with sample.pdf to Server A test/servicebus-test" && \
	echo "Polling Server B for Service Bus file relay (up to 60s)..." && \
	SB_FILE_FOUND="FAIL" && \
	for i in $$(seq 1 60); do \
		sleep 1; \
		SB_FILE_FOUND=$$(curl -sf "http://$(MM_HOST):$(MM_PORT_B)/api/v4/channels/$$SB_B/posts?per_page=10" \
			-H "Authorization: Bearer $$TOKEN_B" | python3 -c "import sys,json;d=json.load(sys.stdin);sid='$$SB_FILE_ID';m=[p for p in d.get('posts',{}).values() if 'servicebus-file-test:'+sid in p.get('message','')];files=(m[0].get('metadata') or {}).get('files') or [] if m else [];print('PASS' if m and len(files)>0 else 'FAIL')" 2>/dev/null || echo "FAIL"); \
		[ "$$SB_FILE_FOUND" = "PASS" ] && break; \
	done && \
	echo "Service Bus file relay test: $$SB_FILE_FOUND" && \
	[ "$$SB_FILE_FOUND" = "PASS" ] || \
	{ echo "Service Bus file relay FAILED: servicebus-file-test:$$SB_FILE_ID not found with attachments on Server B test/servicebus-test"; exit 1; }

## Post lifecycle test: edits, deletes, reactions on the smoke test channel.
## Requires the smoke test to have run (the low-to-high channel must be linked
## end-to-end). Resolves the B-side post id once by message content, then drives
## edit / reaction-add / reaction-remove / delete on Server A and verifies each
## change propagates to the same post on Server B.
.PHONY: docker-post-lifecycle-test
docker-post-lifecycle-test: docker-check
	@echo ""
	@echo "Running cross-server post lifecycle test..."
	@TOKEN_A=$$(curl -sf -X POST http://$(MM_HOST):$(MM_PORT_A)/api/v4/users/login \
		-d '{"login_id":"admin","password":"password"}' -i 2>/dev/null \
		| grep -i '^Token:' | awk '{print $$2}' | tr -d '\r') && \
	TOKEN_B=$$(curl -sf -X POST http://$(MM_HOST):$(MM_PORT_B)/api/v4/users/login \
		-d '{"login_id":"admin","password":"password"}' -i 2>/dev/null \
		| grep -i '^Token:' | awk '{print $$2}' | tr -d '\r') && \
	TOKEN_USERA=$$(curl -sf -X POST http://$(MM_HOST):$(MM_PORT_A)/api/v4/users/login \
		-d '{"login_id":"usera","password":"password"}' -i 2>/dev/null \
		| grep -i '^Token:' | awk '{print $$2}' | tr -d '\r') && \
	LTH_A=$$(curl -sf http://$(MM_HOST):$(MM_PORT_A)/api/v4/teams/name/test/channels/name/low-to-high \
		-H "Authorization: Bearer $$TOKEN_A" | python3 -c "import sys,json; print(json.load(sys.stdin)['id'])") && \
	LTH_B=$$(curl -sf http://$(MM_HOST):$(MM_PORT_B)/api/v4/teams/name/test/channels/name/low-to-high \
		-H "Authorization: Bearer $$TOKEN_B" | python3 -c "import sys,json; print(json.load(sys.stdin)['id'])") && \
	USERA_ID=$$(curl -sf http://$(MM_HOST):$(MM_PORT_A)/api/v4/users/username/usera \
		-H "Authorization: Bearer $$TOKEN_A" | python3 -c "import sys,json; print(json.load(sys.stdin)['id'])") && \
	LIFE_ID=$$(date +%s)-$$$$-life && \
	echo "Posting lifecycle base message from Server A as usera..." && \
	POST_A=$$(curl -sf -X POST http://$(MM_HOST):$(MM_PORT_A)/api/v4/posts \
		-H "Authorization: Bearer $$TOKEN_USERA" \
		-H "Content-Type: application/json" \
		-d '{"channel_id":"'"$$LTH_A"'","message":"lifecycle-test:'"$$LIFE_ID"'"}' \
		| python3 -c "import sys,json; print(json.load(sys.stdin)['id'])") && \
	echo "  Posted lifecycle-test:$$LIFE_ID (A post id: $$POST_A)" && \
	echo "Polling Server B for the relayed post id (up to 20s)..." && \
	POST_B="" && \
	for i in $$(seq 1 20); do \
		sleep 1; \
		POST_B=$$(curl -sf "http://$(MM_HOST):$(MM_PORT_B)/api/v4/channels/$$LTH_B/posts?per_page=20" \
			-H "Authorization: Bearer $$TOKEN_B" | python3 -c "import sys,json;d=json.load(sys.stdin);sid='$$LIFE_ID';ids=[p['id'] for p in d.get('posts',{}).values() if 'lifecycle-test:'+sid in p.get('message','')];print(ids[0] if ids else '')"); \
		[ -n "$$POST_B" ] && break; \
	done && \
	[ -n "$$POST_B" ] || { echo "Initial relay FAILED: lifecycle-test:$$LIFE_ID not found on Server B"; exit 1; } && \
	echo "  Initial relay PASS (B post id: $$POST_B)" && \
	echo "Editing the post on Server A..." && \
	curl -sf -X PUT http://$(MM_HOST):$(MM_PORT_A)/api/v4/posts/$$POST_A/patch \
		-H "Authorization: Bearer $$TOKEN_USERA" \
		-H "Content-Type: application/json" \
		-d '{"message":"lifecycle-test:'"$$LIFE_ID"' edited"}' >/dev/null && \
	echo "  Edit issued; polling Server B for update (up to 20s)..." && \
	EDIT_OK="FAIL" && \
	for i in $$(seq 1 20); do \
		sleep 1; \
		EDIT_OK=$$(curl -sf "http://$(MM_HOST):$(MM_PORT_B)/api/v4/posts/$$POST_B" \
			-H "Authorization: Bearer $$TOKEN_B" | python3 -c "import sys,json;p=json.load(sys.stdin);m=p.get('message','');u=p.get('update_at',0);c=p.get('create_at',0);print('PASS' if 'edited' in m and u>c else 'FAIL')" 2>/dev/null || echo "FAIL"); \
		[ "$$EDIT_OK" = "PASS" ] && break; \
	done && \
	echo "  Edit relay test: $$EDIT_OK" && \
	[ "$$EDIT_OK" = "PASS" ] || { echo "Edit relay FAILED: post $$POST_B on Server B did not reflect edit"; exit 1; } && \
	echo "Adding reaction (thumbsup) on Server A..." && \
	curl -sf -X POST http://$(MM_HOST):$(MM_PORT_A)/api/v4/reactions \
		-H "Authorization: Bearer $$TOKEN_USERA" \
		-H "Content-Type: application/json" \
		-d '{"user_id":"'"$$USERA_ID"'","post_id":"'"$$POST_A"'","emoji_name":"thumbsup"}' >/dev/null && \
	echo "  Reaction issued; polling Server B (up to 20s)..." && \
	REACT_OK="FAIL" && \
	for i in $$(seq 1 20); do \
		sleep 1; \
		REACT_OK=$$(curl -sf "http://$(MM_HOST):$(MM_PORT_B)/api/v4/posts/$$POST_B/reactions" \
			-H "Authorization: Bearer $$TOKEN_B" | python3 -c "import sys,json;r=json.load(sys.stdin);print('PASS' if any(x.get('emoji_name')=='thumbsup' for x in (r or [])) else 'FAIL')" 2>/dev/null || echo "FAIL"); \
		[ "$$REACT_OK" = "PASS" ] && break; \
	done && \
	echo "  Reaction add relay test: $$REACT_OK" && \
	[ "$$REACT_OK" = "PASS" ] || { echo "Reaction add relay FAILED: thumbsup not present on $$POST_B"; exit 1; } && \
	echo "Removing reaction (thumbsup) on Server A..." && \
	curl -sf -X DELETE http://$(MM_HOST):$(MM_PORT_A)/api/v4/users/$$USERA_ID/posts/$$POST_A/reactions/thumbsup \
		-H "Authorization: Bearer $$TOKEN_USERA" >/dev/null && \
	echo "  Reaction removal issued; polling Server B (up to 20s)..." && \
	UNREACT_OK="FAIL" && \
	for i in $$(seq 1 20); do \
		sleep 1; \
		UNREACT_OK=$$(curl -sf "http://$(MM_HOST):$(MM_PORT_B)/api/v4/posts/$$POST_B/reactions" \
			-H "Authorization: Bearer $$TOKEN_B" | python3 -c "import sys,json;r=json.load(sys.stdin);print('PASS' if not any(x.get('emoji_name')=='thumbsup' for x in (r or [])) else 'FAIL')" 2>/dev/null || echo "FAIL"); \
		[ "$$UNREACT_OK" = "PASS" ] && break; \
	done && \
	echo "  Reaction remove relay test: $$UNREACT_OK" && \
	[ "$$UNREACT_OK" = "PASS" ] || { echo "Reaction remove relay FAILED: thumbsup still present on $$POST_B"; exit 1; } && \
	echo "Deleting the post on Server A..." && \
	curl -sf -X DELETE http://$(MM_HOST):$(MM_PORT_A)/api/v4/posts/$$POST_A \
		-H "Authorization: Bearer $$TOKEN_USERA" >/dev/null && \
	echo "  Delete issued; polling Server B (up to 20s)..." && \
	DEL_OK="FAIL" && \
	for i in $$(seq 1 20); do \
		sleep 1; \
		DEL_OK=$$(curl -sf "http://$(MM_HOST):$(MM_PORT_B)/api/v4/posts/$$POST_B?include_deleted=true" \
			-H "Authorization: Bearer $$TOKEN_B" | python3 -c "import sys,json;p=json.load(sys.stdin);print('PASS' if p.get('delete_at',0)>0 else 'FAIL')" 2>/dev/null || echo "FAIL"); \
		[ "$$DEL_OK" = "PASS" ] && break; \
	done && \
	echo "  Delete relay test: $$DEL_OK" && \
	[ "$$DEL_OK" = "PASS" ] || { echo "Delete relay FAILED: post $$POST_B not marked deleted on Server B"; exit 1; }

## Profile image sync test: uploads a new avatar for a dedicated user on
## Server A and verifies the framework propagates the change to the sync user
## on Server B (last_picture_update must advance). Uses a fresh user (userg)
## so it does not interfere with the smoke test's usera sync ownership.
.PHONY: docker-profile-image-test
docker-profile-image-test: docker-check
	@echo ""
	@echo "Running cross-server profile image sync test..."
	@echo "Creating dedicated user (userg) on Server A..."
	@$(DOCKER_COMPOSE) exec -T mattermost-a mmctl --local user create \
		--email userg@example.com --username userg --password 'password' 2>/dev/null \
		|| echo "  User userg already exists on Server A"
	@$(DOCKER_COMPOSE) exec -T mattermost-a mmctl --local team users add test userg 2>/dev/null || true
	@TOKEN_A=$$(curl -sf -X POST http://$(MM_HOST):$(MM_PORT_A)/api/v4/users/login \
		-d '{"login_id":"admin","password":"password"}' -i 2>/dev/null \
		| grep -i '^Token:' | awk '{print $$2}' | tr -d '\r') && \
	TOKEN_B=$$(curl -sf -X POST http://$(MM_HOST):$(MM_PORT_B)/api/v4/users/login \
		-d '{"login_id":"admin","password":"password"}' -i 2>/dev/null \
		| grep -i '^Token:' | awk '{print $$2}' | tr -d '\r') && \
	TOKEN_USERG=$$(curl -sf -X POST http://$(MM_HOST):$(MM_PORT_A)/api/v4/users/login \
		-d '{"login_id":"userg","password":"password"}' -i 2>/dev/null \
		| grep -i '^Token:' | awk '{print $$2}' | tr -d '\r') && \
	LTH_A=$$(curl -sf http://$(MM_HOST):$(MM_PORT_A)/api/v4/teams/name/test/channels/name/low-to-high \
		-H "Authorization: Bearer $$TOKEN_A" | python3 -c "import sys,json; print(json.load(sys.stdin)['id'])") && \
	USERG_ID=$$(curl -sf http://$(MM_HOST):$(MM_PORT_A)/api/v4/users/username/userg \
		-H "Authorization: Bearer $$TOKEN_A" | python3 -c "import sys,json; print(json.load(sys.stdin)['id'])") && \
	echo "Adding userg to test/low-to-high channel..." && \
	curl -sf -X POST http://$(MM_HOST):$(MM_PORT_A)/api/v4/channels/$$LTH_A/members \
		-H "Authorization: Bearer $$TOKEN_A" -H "Content-Type: application/json" \
		-d '{"user_id":"'"$$USERG_ID"'"}' >/dev/null 2>&1 || true && \
	echo "Posting warmup message from userg (triggers framework user sync to Server B)..." && \
	WARM_ID=$$(date +%s)-$$$$-prof && \
	curl -sf -X POST http://$(MM_HOST):$(MM_PORT_A)/api/v4/posts \
		-H "Authorization: Bearer $$TOKEN_USERG" \
		-H "Content-Type: application/json" \
		-d '{"channel_id":"'"$$LTH_A"'","message":"profile-warmup:'"$$WARM_ID"'"}' >/dev/null && \
	echo "  Polling Server B for sync user userg:cross-guard--low-to-high (up to 30s)..." && \
	SYNC_USER_ID="" && INITIAL_LPU="0" && \
	for i in $$(seq 1 30); do \
		sleep 1; \
		SYNC_INFO=$$(curl -sf "http://$(MM_HOST):$(MM_PORT_B)/api/v4/users?per_page=200" \
			-H "Authorization: Bearer $$TOKEN_B" | python3 -c "import sys,json;users=json.load(sys.stdin);m=[u for u in users if u.get('username','').startswith('userg:')];print(m[0]['id']+' '+str(m[0].get('last_picture_update',0)) if m else '')" 2>/dev/null || echo ""); \
		[ -n "$$SYNC_INFO" ] && SYNC_USER_ID=$$(echo $$SYNC_INFO | awk '{print $$1}') && INITIAL_LPU=$$(echo $$SYNC_INFO | awk '{print $$2}') && break; \
	done && \
	[ -n "$$SYNC_USER_ID" ] || { echo "Profile sync setup FAILED: userg sync user not found on Server B"; exit 1; } && \
	echo "  Sync user found: id=$$SYNC_USER_ID last_picture_update=$$INITIAL_LPU" && \
	echo "Generating unique profile image (forces a new last_picture_update)..." && \
	python3 build/gen-unique-png.py > /tmp/userg-unique-image.png && \
	echo "Uploading new profile image for userg on Server A..." && \
	curl -sf -X POST http://$(MM_HOST):$(MM_PORT_A)/api/v4/users/$$USERG_ID/image \
		-H "Authorization: Bearer $$TOKEN_USERG" \
		-F "image=@/tmp/userg-unique-image.png" >/dev/null && \
	echo "  Image uploaded; polling Server B for last_picture_update advance (up to 90s)..." && \
	LPU_OK="FAIL" && NEW_LPU="$$INITIAL_LPU" && \
	for i in $$(seq 1 90); do \
		sleep 1; \
		NEW_LPU=$$(curl -sf "http://$(MM_HOST):$(MM_PORT_B)/api/v4/users/$$SYNC_USER_ID" \
			-H "Authorization: Bearer $$TOKEN_B" | python3 -c "import sys,json; print(json.load(sys.stdin).get('last_picture_update',0))" 2>/dev/null || echo "$$INITIAL_LPU"); \
		if [ "$$NEW_LPU" -gt "$$INITIAL_LPU" ] 2>/dev/null; then LPU_OK="PASS"; break; fi; \
	done && \
	echo "  Profile image sync test: $$LPU_OK (initial=$$INITIAL_LPU, new=$$NEW_LPU)" && \
	[ "$$LPU_OK" = "PASS" ] || { echo "Profile image sync FAILED: last_picture_update did not advance on Server B"; exit 1; }

## File filter test: verify sender-side and receiver-side file_filter_mode=deny
## drops blocked attachments while still relaying the post body. Cleans up
## (clears filter, resets plugin) at the end of each sub-test so subsequent
## tests find an unfiltered low-to-high connection.
## NOTE: sub-test C from the implementation plan (per-connection max_file_size)
## is not implementable: the plugin reads the server's global
## FileSettings.MaxFileSize, not a per-connection field. Size enforcement is
## covered by Go unit tests in hooks_test.go.
.PHONY: docker-file-filter-test
docker-file-filter-test: docker-check
	@echo ""
	@echo "Running cross-server file filter test..."
	@TOKEN_A=$$(curl -sf -X POST http://$(MM_HOST):$(MM_PORT_A)/api/v4/users/login \
		-d '{"login_id":"admin","password":"password"}' -i 2>/dev/null \
		| grep -i '^Token:' | awk '{print $$2}' | tr -d '\r') && \
	TOKEN_B=$$(curl -sf -X POST http://$(MM_HOST):$(MM_PORT_B)/api/v4/users/login \
		-d '{"login_id":"admin","password":"password"}' -i 2>/dev/null \
		| grep -i '^Token:' | awk '{print $$2}' | tr -d '\r') && \
	TOKEN_USERA=$$(curl -sf -X POST http://$(MM_HOST):$(MM_PORT_A)/api/v4/users/login \
		-d '{"login_id":"usera","password":"password"}' -i 2>/dev/null \
		| grep -i '^Token:' | awk '{print $$2}' | tr -d '\r') && \
	LTH_A=$$(curl -sf http://$(MM_HOST):$(MM_PORT_A)/api/v4/teams/name/test/channels/name/low-to-high \
		-H "Authorization: Bearer $$TOKEN_A" | python3 -c "import sys,json; print(json.load(sys.stdin)['id'])") && \
	LTH_B=$$(curl -sf http://$(MM_HOST):$(MM_PORT_B)/api/v4/teams/name/test/channels/name/low-to-high \
		-H "Authorization: Bearer $$TOKEN_B" | python3 -c "import sys,json; print(json.load(sys.stdin)['id'])") && \
	echo "--- Sub-test A: sender-side deny .pdf ---" && \
	echo "Setting file_filter_mode=deny, file_filter_types=.pdf on Server A outbound low-to-high..." && \
	EXISTING_OB_A=$$(curl -sf http://$(MM_HOST):$(MM_PORT_A)/api/v4/config \
		-H "Authorization: Bearer $$TOKEN_A" | python3 -c "import sys,json; c=json.load(sys.stdin); ps=c.get('PluginSettings',{}).get('Plugins',{}).get('crossguard',{}); print(ps.get('outboundconnections','[]'))") && \
	NEW_OB_A=$$(python3 -c "import sys,json; e=json.loads('$$EXISTING_OB_A'); [c.update({'file_filter_mode':'deny','file_filter_types':'.pdf'}) for c in e if c.get('name')=='low-to-high']; print(json.dumps(e))") && \
	curl -sf -X PUT http://$(MM_HOST):$(MM_PORT_A)/api/v4/config/patch \
		-H "Authorization: Bearer $$TOKEN_A" -H "Content-Type: application/json" \
		-d '{"PluginSettings":{"Plugins":{"crossguard":{"outboundconnections":"'"$$(echo $$NEW_OB_A | sed 's/"/\\"/g')"'"}}}}' >/dev/null && \
	$(DOCKER_COMPOSE) exec -T mattermost-a mmctl --local plugin disable $(PLUGIN_ID) >/dev/null && \
	$(DOCKER_COMPOSE) exec -T mattermost-a mmctl --local plugin enable $(PLUGIN_ID) >/dev/null && \
	sleep 3 && \
	echo "  Server A plugin reset (filter=deny .pdf)" && \
	FA_DENY_ID=$$(date +%s)-$$$$-fad && \
	FILE_UPLOAD=$$(curl -sf -X POST "http://$(MM_HOST):$(MM_PORT_A)/api/v4/files?channel_id=$$LTH_A" \
		-H "Authorization: Bearer $$TOKEN_USERA" -F "files=@testdata/sample.pdf" \
		| python3 -c "import sys,json; print(json.load(sys.stdin)['file_infos'][0]['id'])") && \
	curl -sf -X POST http://$(MM_HOST):$(MM_PORT_A)/api/v4/posts \
		-H "Authorization: Bearer $$TOKEN_USERA" -H "Content-Type: application/json" \
		-d '{"channel_id":"'"$$LTH_A"'","message":"filter-deny:'"$$FA_DENY_ID"'","file_ids":["'"$$FILE_UPLOAD"'"]}' >/dev/null && \
	echo "  Posted filter-deny:$$FA_DENY_ID with PDF; polling B (file should be filtered, up to 20s)..." && \
	FA_DENY_OK="FAIL" && \
	for i in $$(seq 1 20); do \
		sleep 1; \
		FA_DENY_OK=$$(curl -sf "http://$(MM_HOST):$(MM_PORT_B)/api/v4/channels/$$LTH_B/posts?per_page=20" \
			-H "Authorization: Bearer $$TOKEN_B" | python3 -c "import sys,json;d=json.load(sys.stdin);sid='$$FA_DENY_ID';m=[p for p in d.get('posts',{}).values() if 'filter-deny:'+sid in p.get('message','')];files=(m[0].get('metadata') or {}).get('files') or [] if m else [];print('PASS' if m and len(files)==0 else ('FAIL_WITH_FILES' if m else 'NOT_YET'))" 2>/dev/null || echo "NOT_YET"); \
		[ "$$FA_DENY_OK" = "PASS" ] && break; \
		[ "$$FA_DENY_OK" = "FAIL_WITH_FILES" ] && break; \
	done && \
	echo "  Sub-test A (sender deny .pdf): $$FA_DENY_OK" && \
	[ "$$FA_DENY_OK" = "PASS" ] || { echo "Sub-test A FAILED: PDF reached Server B despite sender deny filter"; exit 1; } && \
	echo "Switching filter to .txt (PDF now allowed)..." && \
	EXISTING_OB_A=$$(curl -sf http://$(MM_HOST):$(MM_PORT_A)/api/v4/config \
		-H "Authorization: Bearer $$TOKEN_A" | python3 -c "import sys,json; c=json.load(sys.stdin); ps=c.get('PluginSettings',{}).get('Plugins',{}).get('crossguard',{}); print(ps.get('outboundconnections','[]'))") && \
	NEW_OB_A=$$(python3 -c "import sys,json; e=json.loads('$$EXISTING_OB_A'); [c.update({'file_filter_mode':'deny','file_filter_types':'.txt'}) for c in e if c.get('name')=='low-to-high']; print(json.dumps(e))") && \
	curl -sf -X PUT http://$(MM_HOST):$(MM_PORT_A)/api/v4/config/patch \
		-H "Authorization: Bearer $$TOKEN_A" -H "Content-Type: application/json" \
		-d '{"PluginSettings":{"Plugins":{"crossguard":{"outboundconnections":"'"$$(echo $$NEW_OB_A | sed 's/"/\\"/g')"'"}}}}' >/dev/null && \
	$(DOCKER_COMPOSE) exec -T mattermost-a mmctl --local plugin disable $(PLUGIN_ID) >/dev/null && \
	$(DOCKER_COMPOSE) exec -T mattermost-a mmctl --local plugin enable $(PLUGIN_ID) >/dev/null && \
	sleep 3 && \
	FA_ALLOW_ID=$$(date +%s)-$$$$-faa && \
	FILE_UPLOAD2=$$(curl -sf -X POST "http://$(MM_HOST):$(MM_PORT_A)/api/v4/files?channel_id=$$LTH_A" \
		-H "Authorization: Bearer $$TOKEN_USERA" -F "files=@testdata/sample.pdf" \
		| python3 -c "import sys,json; print(json.load(sys.stdin)['file_infos'][0]['id'])") && \
	curl -sf -X POST http://$(MM_HOST):$(MM_PORT_A)/api/v4/posts \
		-H "Authorization: Bearer $$TOKEN_USERA" -H "Content-Type: application/json" \
		-d '{"channel_id":"'"$$LTH_A"'","message":"filter-allow:'"$$FA_ALLOW_ID"'","file_ids":["'"$$FILE_UPLOAD2"'"]}' >/dev/null && \
	echo "  Posted filter-allow:$$FA_ALLOW_ID with PDF; polling B (file should relay, up to 20s)..." && \
	FA_ALLOW_OK="FAIL" && \
	for i in $$(seq 1 20); do \
		sleep 1; \
		FA_ALLOW_OK=$$(curl -sf "http://$(MM_HOST):$(MM_PORT_B)/api/v4/channels/$$LTH_B/posts?per_page=20" \
			-H "Authorization: Bearer $$TOKEN_B" | python3 -c "import sys,json;d=json.load(sys.stdin);sid='$$FA_ALLOW_ID';m=[p for p in d.get('posts',{}).values() if 'filter-allow:'+sid in p.get('message','')];files=(m[0].get('metadata') or {}).get('files') or [] if m else [];print('PASS' if m and len(files)>0 else 'FAIL')" 2>/dev/null || echo "FAIL"); \
		[ "$$FA_ALLOW_OK" = "PASS" ] && break; \
	done && \
	echo "  Sub-test A negative (filter=.txt, PDF allowed): $$FA_ALLOW_OK" && \
	[ "$$FA_ALLOW_OK" = "PASS" ] || { echo "Sub-test A negative FAILED: PDF should have relayed when filter excluded other extensions"; exit 1; } && \
	echo "Restoring Server A: clearing sender filter..." && \
	EXISTING_OB_A=$$(curl -sf http://$(MM_HOST):$(MM_PORT_A)/api/v4/config \
		-H "Authorization: Bearer $$TOKEN_A" | python3 -c "import sys,json; c=json.load(sys.stdin); ps=c.get('PluginSettings',{}).get('Plugins',{}).get('crossguard',{}); print(ps.get('outboundconnections','[]'))") && \
	NEW_OB_A=$$(python3 -c "import sys,json; e=json.loads('$$EXISTING_OB_A'); [c.update({'file_filter_mode':'','file_filter_types':''}) for c in e if c.get('name')=='low-to-high']; print(json.dumps(e))") && \
	curl -sf -X PUT http://$(MM_HOST):$(MM_PORT_A)/api/v4/config/patch \
		-H "Authorization: Bearer $$TOKEN_A" -H "Content-Type: application/json" \
		-d '{"PluginSettings":{"Plugins":{"crossguard":{"outboundconnections":"'"$$(echo $$NEW_OB_A | sed 's/"/\\"/g')"'"}}}}' >/dev/null && \
	$(DOCKER_COMPOSE) exec -T mattermost-a mmctl --local plugin disable $(PLUGIN_ID) >/dev/null && \
	$(DOCKER_COMPOSE) exec -T mattermost-a mmctl --local plugin enable $(PLUGIN_ID) >/dev/null && \
	sleep 3 && \
	echo "--- Sub-test B: receiver-side deny .pdf ---" && \
	echo "Setting file_filter_mode=deny, file_filter_types=.pdf on Server B inbound low-to-high..." && \
	EXISTING_IB_B=$$(curl -sf http://$(MM_HOST):$(MM_PORT_B)/api/v4/config \
		-H "Authorization: Bearer $$TOKEN_B" | python3 -c "import sys,json; c=json.load(sys.stdin); ps=c.get('PluginSettings',{}).get('Plugins',{}).get('crossguard',{}); print(ps.get('inboundconnections','[]'))") && \
	NEW_IB_B=$$(python3 -c "import sys,json; e=json.loads('$$EXISTING_IB_B'); [c.update({'file_filter_mode':'deny','file_filter_types':'.pdf'}) for c in e if c.get('name')=='low-to-high']; print(json.dumps(e))") && \
	curl -sf -X PUT http://$(MM_HOST):$(MM_PORT_B)/api/v4/config/patch \
		-H "Authorization: Bearer $$TOKEN_B" -H "Content-Type: application/json" \
		-d '{"PluginSettings":{"Plugins":{"crossguard":{"inboundconnections":"'"$$(echo $$NEW_IB_B | sed 's/"/\\"/g')"'"}}}}' >/dev/null && \
	$(DOCKER_COMPOSE) exec -T mattermost-b mmctl --local plugin disable $(PLUGIN_ID) >/dev/null && \
	$(DOCKER_COMPOSE) exec -T mattermost-b mmctl --local plugin enable $(PLUGIN_ID) >/dev/null && \
	sleep 3 && \
	echo "  Server B plugin reset (receiver filter=deny .pdf)" && \
	FB_DENY_ID=$$(date +%s)-$$$$-fbd && \
	FILE_UPLOAD3=$$(curl -sf -X POST "http://$(MM_HOST):$(MM_PORT_A)/api/v4/files?channel_id=$$LTH_A" \
		-H "Authorization: Bearer $$TOKEN_USERA" -F "files=@testdata/sample.pdf" \
		| python3 -c "import sys,json; print(json.load(sys.stdin)['file_infos'][0]['id'])") && \
	curl -sf -X POST http://$(MM_HOST):$(MM_PORT_A)/api/v4/posts \
		-H "Authorization: Bearer $$TOKEN_USERA" -H "Content-Type: application/json" \
		-d '{"channel_id":"'"$$LTH_A"'","message":"filter-recv-deny:'"$$FB_DENY_ID"'","file_ids":["'"$$FILE_UPLOAD3"'"]}' >/dev/null && \
	echo "  Posted filter-recv-deny:$$FB_DENY_ID with PDF; polling B (file should be filtered by receiver, up to 20s)..." && \
	FB_DENY_OK="FAIL" && \
	for i in $$(seq 1 20); do \
		sleep 1; \
		FB_DENY_OK=$$(curl -sf "http://$(MM_HOST):$(MM_PORT_B)/api/v4/channels/$$LTH_B/posts?per_page=20" \
			-H "Authorization: Bearer $$TOKEN_B" | python3 -c "import sys,json;d=json.load(sys.stdin);sid='$$FB_DENY_ID';m=[p for p in d.get('posts',{}).values() if 'filter-recv-deny:'+sid in p.get('message','')];files=(m[0].get('metadata') or {}).get('files') or [] if m else [];print('PASS' if m and len(files)==0 else ('FAIL_WITH_FILES' if m else 'NOT_YET'))" 2>/dev/null || echo "NOT_YET"); \
		[ "$$FB_DENY_OK" = "PASS" ] && break; \
		[ "$$FB_DENY_OK" = "FAIL_WITH_FILES" ] && break; \
	done && \
	echo "  Sub-test B (receiver deny .pdf): $$FB_DENY_OK" && \
	echo "Restoring Server B: clearing receiver filter..." && \
	EXISTING_IB_B=$$(curl -sf http://$(MM_HOST):$(MM_PORT_B)/api/v4/config \
		-H "Authorization: Bearer $$TOKEN_B" | python3 -c "import sys,json; c=json.load(sys.stdin); ps=c.get('PluginSettings',{}).get('Plugins',{}).get('crossguard',{}); print(ps.get('inboundconnections','[]'))") && \
	NEW_IB_B=$$(python3 -c "import sys,json; e=json.loads('$$EXISTING_IB_B'); [c.update({'file_filter_mode':'','file_filter_types':''}) for c in e if c.get('name')=='low-to-high']; print(json.dumps(e))") && \
	curl -sf -X PUT http://$(MM_HOST):$(MM_PORT_B)/api/v4/config/patch \
		-H "Authorization: Bearer $$TOKEN_B" -H "Content-Type: application/json" \
		-d '{"PluginSettings":{"Plugins":{"crossguard":{"inboundconnections":"'"$$(echo $$NEW_IB_B | sed 's/"/\\"/g')"'"}}}}' >/dev/null && \
	$(DOCKER_COMPOSE) exec -T mattermost-b mmctl --local plugin disable $(PLUGIN_ID) >/dev/null && \
	$(DOCKER_COMPOSE) exec -T mattermost-b mmctl --local plugin enable $(PLUGIN_ID) >/dev/null && \
	sleep 3 && \
	[ "$$FB_DENY_OK" = "PASS" ] || { echo "Sub-test B FAILED: PDF reached Server B despite receiver deny filter (state: $$FB_DENY_OK)"; exit 1; } && \
	echo "  Receiver filter cleared, Server B plugin reset"

## Connection prompt accept/block test: drives the first-time-link prompt
## flow at the channel level. Uses a fresh channel name per run so prompt KV
## state from previous runs does not mask new prompts. Requires the team to
## already be linked (the smoke test does this for low-to-high on Server B).
## Each sub-test uses its own channel name so the accept and block flows
## stay independent.
.PHONY: docker-prompt-test
docker-prompt-test: docker-check
	@echo ""
	@echo "Running cross-server channel prompt accept/block test..."
	@TOKEN_A=$$(curl -sf -X POST http://$(MM_HOST):$(MM_PORT_A)/api/v4/users/login \
		-d '{"login_id":"admin","password":"password"}' -i 2>/dev/null \
		| grep -i '^Token:' | awk '{print $$2}' | tr -d '\r') && \
	TOKEN_B=$$(curl -sf -X POST http://$(MM_HOST):$(MM_PORT_B)/api/v4/users/login \
		-d '{"login_id":"admin","password":"password"}' -i 2>/dev/null \
		| grep -i '^Token:' | awk '{print $$2}' | tr -d '\r') && \
	TOKEN_USERA=$$(curl -sf -X POST http://$(MM_HOST):$(MM_PORT_A)/api/v4/users/login \
		-d '{"login_id":"usera","password":"password"}' -i 2>/dev/null \
		| grep -i '^Token:' | awk '{print $$2}' | tr -d '\r') && \
	TEAM_A=$$(curl -sf http://$(MM_HOST):$(MM_PORT_A)/api/v4/teams/name/test \
		-H "Authorization: Bearer $$TOKEN_A" | python3 -c "import sys,json; print(json.load(sys.stdin)['id'])") && \
	TEAM_B=$$(curl -sf http://$(MM_HOST):$(MM_PORT_B)/api/v4/teams/name/test \
		-H "Authorization: Bearer $$TOKEN_B" | python3 -c "import sys,json; print(json.load(sys.stdin)['id'])") && \
	USERA_ID=$$(curl -sf http://$(MM_HOST):$(MM_PORT_A)/api/v4/users/username/usera \
		-H "Authorization: Bearer $$TOKEN_A" | python3 -c "import sys,json; print(json.load(sys.stdin)['id'])") && \
	ADMIN_B_ID=$$(curl -sf http://$(MM_HOST):$(MM_PORT_B)/api/v4/users/username/admin \
		-H "Authorization: Bearer $$TOKEN_B" | python3 -c "import sys,json; print(json.load(sys.stdin)['id'])") && \
	RUN_ID=$$(date +%s)-$$$$ && \
	echo "--- Accept path ---" && \
	ACC_NAME="prompt-accept-$$RUN_ID" && \
	echo "Creating channel $$ACC_NAME on both servers..." && \
	ACC_A=$$(curl -sf -X POST http://$(MM_HOST):$(MM_PORT_A)/api/v4/channels \
		-H "Authorization: Bearer $$TOKEN_A" -H "Content-Type: application/json" \
		-d '{"team_id":"'"$$TEAM_A"'","name":"'"$$ACC_NAME"'","display_name":"'"$$ACC_NAME"'","type":"O"}' \
		| python3 -c "import sys,json; print(json.load(sys.stdin)['id'])") && \
	ACC_B=$$(curl -sf -X POST http://$(MM_HOST):$(MM_PORT_B)/api/v4/channels \
		-H "Authorization: Bearer $$TOKEN_B" -H "Content-Type: application/json" \
		-d '{"team_id":"'"$$TEAM_B"'","name":"'"$$ACC_NAME"'","display_name":"'"$$ACC_NAME"'","type":"O"}' \
		| python3 -c "import sys,json; print(json.load(sys.stdin)['id'])") && \
	echo "  Server A: $$ACC_A; Server B: $$ACC_B" && \
	curl -sf -X POST http://$(MM_HOST):$(MM_PORT_A)/api/v4/channels/$$ACC_A/members \
		-H "Authorization: Bearer $$TOKEN_A" -H "Content-Type: application/json" \
		-d '{"user_id":"'"$$USERA_ID"'"}' >/dev/null 2>&1 || true && \
	echo "Linking channel on Server A (outbound only)..." && \
	curl -sf -X POST http://$(MM_HOST):$(MM_PORT_A)/api/v4/commands/execute \
		-H "Authorization: Bearer $$TOKEN_A" -H "Content-Type: application/json" \
		-d '{"channel_id":"'"$$ACC_A"'","command":"/crossguard init-channel outbound:low-to-high"}' >/dev/null && \
	echo "Posting trigger message on Server A (should NOT relay; should trigger prompt on B)..." && \
	ACC_TRIG=$$(date +%s)-$$$$-acc-trig && \
	curl -sf -X POST http://$(MM_HOST):$(MM_PORT_A)/api/v4/posts \
		-H "Authorization: Bearer $$TOKEN_USERA" -H "Content-Type: application/json" \
		-d '{"channel_id":"'"$$ACC_A"'","message":"prompt-trigger:'"$$ACC_TRIG"'"}' >/dev/null && \
	echo "Polling Server B channel $$ACC_NAME for prompt post (up to 20s)..." && \
	PROMPT_FOUND="FAIL" && \
	for i in $$(seq 1 20); do \
		sleep 1; \
		PROMPT_FOUND=$$(curl -sf "http://$(MM_HOST):$(MM_PORT_B)/api/v4/channels/$$ACC_B/posts?per_page=20" \
			-H "Authorization: Bearer $$TOKEN_B" | python3 -c "import sys,json;d=json.load(sys.stdin);hits=[p for p in d.get('posts',{}).values() if 'inbound Cross Guard connection' in p.get('message','') and 'low-to-high' in p.get('message','')];print('PASS' if hits else 'FAIL')" 2>/dev/null || echo "FAIL"); \
		[ "$$PROMPT_FOUND" = "PASS" ] && break; \
	done && \
	[ "$$PROMPT_FOUND" = "PASS" ] || { echo "Accept path FAILED: prompt not posted on Server B"; exit 1; } && \
	echo "  Prompt posted on Server B" && \
	echo "Calling channel/accept endpoint..." && \
	ACCEPT_BODY=$$(python3 -c "import json; print(json.dumps({'user_id':'$$ADMIN_B_ID','context':{'channel_id':'$$ACC_B','conn_name':'low-to-high'}}))") && \
	curl -sf -X POST http://$(MM_HOST):$(MM_PORT_B)/plugins/crossguard/api/v1/prompt/channel/accept \
		-H "Authorization: Bearer $$TOKEN_B" -H "Content-Type: application/json" \
		-d "$$ACCEPT_BODY" >/dev/null && \
	sleep 2 && \
	echo "Verifying channel is now linked on Server B..." && \
	LINKED=$$(curl -sf "http://$(MM_HOST):$(MM_PORT_B)/plugins/crossguard/api/v1/channels/$$ACC_B/status" \
		-H "Authorization: Bearer $$TOKEN_B" | python3 -c "import sys,json; d=json.load(sys.stdin); conns=d.get('team_connections',[]); m=[c for c in conns if c.get('name')=='low-to-high' and c.get('direction')=='inbound']; print('PASS' if m and m[0].get('linked') else 'FAIL')") && \
	[ "$$LINKED" = "PASS" ] || { echo "Accept path FAILED: channel not linked after accept (status=$$LINKED)"; exit 1; } && \
	echo "  Channel linked PASS" && \
	echo "Posting follow-up on Server A (should now relay)..." && \
	ACC_FOLLOW=$$(date +%s)-$$$$-acc-follow && \
	curl -sf -X POST http://$(MM_HOST):$(MM_PORT_A)/api/v4/posts \
		-H "Authorization: Bearer $$TOKEN_USERA" -H "Content-Type: application/json" \
		-d '{"channel_id":"'"$$ACC_A"'","message":"prompt-follow:'"$$ACC_FOLLOW"'"}' >/dev/null && \
	FOLLOW_OK="FAIL" && \
	for i in $$(seq 1 20); do \
		sleep 1; \
		FOLLOW_OK=$$(curl -sf "http://$(MM_HOST):$(MM_PORT_B)/api/v4/channels/$$ACC_B/posts?per_page=20" \
			-H "Authorization: Bearer $$TOKEN_B" | python3 -c "import sys,json;d=json.load(sys.stdin);sid='$$ACC_FOLLOW';print('PASS' if any('prompt-follow:'+sid in p.get('message','') for p in d.get('posts',{}).values()) else 'FAIL')" 2>/dev/null || echo "FAIL"); \
		[ "$$FOLLOW_OK" = "PASS" ] && break; \
	done && \
	echo "  Accept path follow-up relay: $$FOLLOW_OK" && \
	[ "$$FOLLOW_OK" = "PASS" ] || { echo "Accept path FAILED: follow-up did not relay"; exit 1; } && \
	echo "--- Block path ---" && \
	BLK_NAME="prompt-block-$$RUN_ID" && \
	echo "Creating channel $$BLK_NAME on both servers..." && \
	BLK_A=$$(curl -sf -X POST http://$(MM_HOST):$(MM_PORT_A)/api/v4/channels \
		-H "Authorization: Bearer $$TOKEN_A" -H "Content-Type: application/json" \
		-d '{"team_id":"'"$$TEAM_A"'","name":"'"$$BLK_NAME"'","display_name":"'"$$BLK_NAME"'","type":"O"}' \
		| python3 -c "import sys,json; print(json.load(sys.stdin)['id'])") && \
	BLK_B=$$(curl -sf -X POST http://$(MM_HOST):$(MM_PORT_B)/api/v4/channels \
		-H "Authorization: Bearer $$TOKEN_B" -H "Content-Type: application/json" \
		-d '{"team_id":"'"$$TEAM_B"'","name":"'"$$BLK_NAME"'","display_name":"'"$$BLK_NAME"'","type":"O"}' \
		| python3 -c "import sys,json; print(json.load(sys.stdin)['id'])") && \
	echo "  Server A: $$BLK_A; Server B: $$BLK_B" && \
	curl -sf -X POST http://$(MM_HOST):$(MM_PORT_A)/api/v4/channels/$$BLK_A/members \
		-H "Authorization: Bearer $$TOKEN_A" -H "Content-Type: application/json" \
		-d '{"user_id":"'"$$USERA_ID"'"}' >/dev/null 2>&1 || true && \
	curl -sf -X POST http://$(MM_HOST):$(MM_PORT_A)/api/v4/commands/execute \
		-H "Authorization: Bearer $$TOKEN_A" -H "Content-Type: application/json" \
		-d '{"channel_id":"'"$$BLK_A"'","command":"/crossguard init-channel outbound:low-to-high"}' >/dev/null && \
	BLK_TRIG=$$(date +%s)-$$$$-blk-trig && \
	curl -sf -X POST http://$(MM_HOST):$(MM_PORT_A)/api/v4/posts \
		-H "Authorization: Bearer $$TOKEN_USERA" -H "Content-Type: application/json" \
		-d '{"channel_id":"'"$$BLK_A"'","message":"prompt-trigger:'"$$BLK_TRIG"'"}' >/dev/null && \
	echo "Polling Server B for prompt on $$BLK_NAME (up to 20s)..." && \
	BLK_PROMPT="FAIL" && \
	for i in $$(seq 1 20); do \
		sleep 1; \
		BLK_PROMPT=$$(curl -sf "http://$(MM_HOST):$(MM_PORT_B)/api/v4/channels/$$BLK_B/posts?per_page=20" \
			-H "Authorization: Bearer $$TOKEN_B" | python3 -c "import sys,json;d=json.load(sys.stdin);hits=[p for p in d.get('posts',{}).values() if 'inbound Cross Guard connection' in p.get('message','') and 'low-to-high' in p.get('message','')];print('PASS' if hits else 'FAIL')" 2>/dev/null || echo "FAIL"); \
		[ "$$BLK_PROMPT" = "PASS" ] && break; \
	done && \
	[ "$$BLK_PROMPT" = "PASS" ] || { echo "Block path FAILED: prompt not posted on Server B"; exit 1; } && \
	echo "Calling channel/block endpoint..." && \
	BLOCK_BODY=$$(python3 -c "import json; print(json.dumps({'user_id':'$$ADMIN_B_ID','context':{'channel_id':'$$BLK_B','conn_name':'low-to-high'}}))") && \
	curl -sf -X POST http://$(MM_HOST):$(MM_PORT_B)/plugins/crossguard/api/v1/prompt/channel/block \
		-H "Authorization: Bearer $$TOKEN_B" -H "Content-Type: application/json" \
		-d "$$BLOCK_BODY" >/dev/null && \
	sleep 2 && \
	echo "Posting follow-up on Server A (should NOT relay)..." && \
	BLK_FOLLOW=$$(date +%s)-$$$$-blk-follow && \
	curl -sf -X POST http://$(MM_HOST):$(MM_PORT_A)/api/v4/posts \
		-H "Authorization: Bearer $$TOKEN_USERA" -H "Content-Type: application/json" \
		-d '{"channel_id":"'"$$BLK_A"'","message":"prompt-follow:'"$$BLK_FOLLOW"'"}' >/dev/null && \
	echo "Waiting 10s and verifying follow-up did NOT relay..." && \
	sleep 10 && \
	BLK_FOLLOW_LEAKED=$$(curl -sf "http://$(MM_HOST):$(MM_PORT_B)/api/v4/channels/$$BLK_B/posts?per_page=20" \
		-H "Authorization: Bearer $$TOKEN_B" | python3 -c "import sys,json;d=json.load(sys.stdin);sid='$$BLK_FOLLOW';print('LEAKED' if any('prompt-follow:'+sid in p.get('message','') for p in d.get('posts',{}).values()) else 'BLOCKED')" 2>/dev/null || echo "BLOCKED") && \
	echo "  Block path follow-up: $$BLK_FOLLOW_LEAKED" && \
	[ "$$BLK_FOLLOW_LEAKED" = "BLOCKED" ] || { echo "Block path FAILED: follow-up relayed despite block"; exit 1; }
