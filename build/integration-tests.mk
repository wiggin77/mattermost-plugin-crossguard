# Integration and smoke test targets for the dual-server Docker dev environment.
# Included from the root Makefile. Relies on variables defined there
# (DOCKER_COMPOSE, MM_PORT_A, MM_PORT_B, PLUGIN_ID) and the docker-check target.

## End-to-end smoke test: init teams/channels, post message on A, verify relay to B
.PHONY: docker-smoke-test
docker-smoke-test: docker-check
	@echo ""
	@echo "Running end-to-end smoke test..."
	@TOKEN_A=$$(curl -sf -X POST http://localhost:$(MM_PORT_A)/api/v4/users/login \
		-d '{"login_id":"admin","password":"password"}' -i 2>/dev/null \
		| grep -i '^Token:' | awk '{print $$2}' | tr -d '\r') && \
	TOKEN_B=$$(curl -sf -X POST http://localhost:$(MM_PORT_B)/api/v4/users/login \
		-d '{"login_id":"admin","password":"password"}' -i 2>/dev/null \
		| grep -i '^Token:' | awk '{print $$2}' | tr -d '\r') && \
	echo "Getting team IDs..." && \
	TEAM_A=$$(curl -sf http://localhost:$(MM_PORT_A)/api/v4/teams/name/test \
		-H "Authorization: Bearer $$TOKEN_A" | python3 -c "import sys,json; print(json.load(sys.stdin)['id'])") && \
	TEAM_B=$$(curl -sf http://localhost:$(MM_PORT_B)/api/v4/teams/name/test \
		-H "Authorization: Bearer $$TOKEN_B" | python3 -c "import sys,json; print(json.load(sys.stdin)['id'])") && \
	echo "Creating dedicated channels..." && \
	LTH_A=$$(curl -sf -X POST http://localhost:$(MM_PORT_A)/api/v4/channels \
		-H "Authorization: Bearer $$TOKEN_A" -H "Content-Type: application/json" \
		-d '{"team_id":"'"$$TEAM_A"'","name":"low-to-high","display_name":"Low To High","type":"O"}' \
		2>/dev/null | python3 -c "import sys,json; print(json.load(sys.stdin)['id'])" 2>/dev/null || \
		curl -sf http://localhost:$(MM_PORT_A)/api/v4/teams/name/test/channels/name/low-to-high \
		-H "Authorization: Bearer $$TOKEN_A" | python3 -c "import sys,json; print(json.load(sys.stdin)['id'])") && \
	echo "  Server A: low-to-high channel ($$LTH_A)" && \
	LTH_B=$$(curl -sf -X POST http://localhost:$(MM_PORT_B)/api/v4/channels \
		-H "Authorization: Bearer $$TOKEN_B" -H "Content-Type: application/json" \
		-d '{"team_id":"'"$$TEAM_B"'","name":"low-to-high","display_name":"Low To High","type":"O"}' \
		2>/dev/null | python3 -c "import sys,json; print(json.load(sys.stdin)['id'])" 2>/dev/null || \
		curl -sf http://localhost:$(MM_PORT_B)/api/v4/teams/name/test/channels/name/low-to-high \
		-H "Authorization: Bearer $$TOKEN_B" | python3 -c "import sys,json; print(json.load(sys.stdin)['id'])") && \
	echo "  Server B: low-to-high channel ($$LTH_B)" && \
	BD_A=$$(curl -sf -X POST http://localhost:$(MM_PORT_A)/api/v4/channels \
		-H "Authorization: Bearer $$TOKEN_A" -H "Content-Type: application/json" \
		-d '{"team_id":"'"$$TEAM_A"'","name":"bi-directional","display_name":"Bi-Directional","type":"O"}' \
		2>/dev/null | python3 -c "import sys,json; print(json.load(sys.stdin)['id'])" 2>/dev/null || \
		curl -sf http://localhost:$(MM_PORT_A)/api/v4/teams/name/test/channels/name/bi-directional \
		-H "Authorization: Bearer $$TOKEN_A" | python3 -c "import sys,json; print(json.load(sys.stdin)['id'])") && \
	echo "  Server A: bi-directional channel ($$BD_A)" && \
	BD_B=$$(curl -sf -X POST http://localhost:$(MM_PORT_B)/api/v4/channels \
		-H "Authorization: Bearer $$TOKEN_B" -H "Content-Type: application/json" \
		-d '{"team_id":"'"$$TEAM_B"'","name":"bi-directional","display_name":"Bi-Directional","type":"O"}' \
		2>/dev/null | python3 -c "import sys,json; print(json.load(sys.stdin)['id'])" 2>/dev/null || \
		curl -sf http://localhost:$(MM_PORT_B)/api/v4/teams/name/test/channels/name/bi-directional \
		-H "Authorization: Bearer $$TOKEN_B" | python3 -c "import sys,json; print(json.load(sys.stdin)['id'])") && \
	echo "  Server B: bi-directional channel ($$BD_B)" && \
	echo "Adding users to channels..." && \
	USERA_ID=$$(curl -sf http://localhost:$(MM_PORT_A)/api/v4/users/username/usera \
		-H "Authorization: Bearer $$TOKEN_A" | python3 -c "import sys,json; print(json.load(sys.stdin)['id'])") && \
	USERAA_ID=$$(curl -sf http://localhost:$(MM_PORT_A)/api/v4/users/username/useraa \
		-H "Authorization: Bearer $$TOKEN_A" | python3 -c "import sys,json; print(json.load(sys.stdin)['id'])") && \
	USERB_ID=$$(curl -sf http://localhost:$(MM_PORT_B)/api/v4/users/username/userb \
		-H "Authorization: Bearer $$TOKEN_B" | python3 -c "import sys,json; print(json.load(sys.stdin)['id'])") && \
	curl -sf -X POST http://localhost:$(MM_PORT_A)/api/v4/channels/$$LTH_A/members \
		-H "Authorization: Bearer $$TOKEN_A" -H "Content-Type: application/json" \
		-d '{"user_id":"'"$$USERA_ID"'"}' >/dev/null 2>&1 || true && \
	curl -sf -X POST http://localhost:$(MM_PORT_A)/api/v4/channels/$$BD_A/members \
		-H "Authorization: Bearer $$TOKEN_A" -H "Content-Type: application/json" \
		-d '{"user_id":"'"$$USERA_ID"'"}' >/dev/null 2>&1 || true && \
	echo "  Server A: usera added to low-to-high, bi-directional" && \
	curl -sf -X POST http://localhost:$(MM_PORT_A)/api/v4/channels/$$LTH_A/members \
		-H "Authorization: Bearer $$TOKEN_A" -H "Content-Type: application/json" \
		-d '{"user_id":"'"$$USERAA_ID"'"}' >/dev/null 2>&1 || true && \
	curl -sf -X POST http://localhost:$(MM_PORT_A)/api/v4/channels/$$BD_A/members \
		-H "Authorization: Bearer $$TOKEN_A" -H "Content-Type: application/json" \
		-d '{"user_id":"'"$$USERAA_ID"'"}' >/dev/null 2>&1 || true && \
	echo "  Server A: useraa added to low-to-high, bi-directional" && \
	curl -sf -X POST http://localhost:$(MM_PORT_B)/api/v4/channels/$$LTH_B/members \
		-H "Authorization: Bearer $$TOKEN_B" -H "Content-Type: application/json" \
		-d '{"user_id":"'"$$USERB_ID"'"}' >/dev/null 2>&1 || true && \
	curl -sf -X POST http://localhost:$(MM_PORT_B)/api/v4/channels/$$BD_B/members \
		-H "Authorization: Bearer $$TOKEN_B" -H "Content-Type: application/json" \
		-d '{"user_id":"'"$$USERB_ID"'"}' >/dev/null 2>&1 || true && \
	echo "  Server B: userb added to low-to-high, bi-directional" && \
	echo "Initializing teams..." && \
	echo "  Waiting 5s for plugin connections to initialize..." && \
	sleep 5 && \
	INIT_RESP=$$(curl -s -X POST http://localhost:$(MM_PORT_A)/api/v4/commands/execute \
		-H "Authorization: Bearer $$TOKEN_A" \
		-H "Content-Type: application/json" \
		-d '{"channel_id":"'"$$LTH_A"'","command":"/crossguard init-team outbound:low-to-high"}') && \
	echo "  Server A: init-team outbound:low-to-high (response: $$INIT_RESP)" && \
	curl -sf -X POST http://localhost:$(MM_PORT_A)/api/v4/commands/execute \
		-H "Authorization: Bearer $$TOKEN_A" \
		-H "Content-Type: application/json" \
		-d '{"channel_id":"'"$$LTH_A"'","command":"/crossguard init-team inbound:high-to-low"}' >/dev/null && \
	echo "  Server A: init-team inbound:high-to-low" && \
	curl -sf -X POST http://localhost:$(MM_PORT_B)/api/v4/commands/execute \
		-H "Authorization: Bearer $$TOKEN_B" \
		-H "Content-Type: application/json" \
		-d '{"channel_id":"'"$$LTH_B"'","command":"/crossguard init-team inbound:low-to-high"}' >/dev/null && \
	echo "  Server B: init-team inbound:low-to-high" && \
	curl -sf -X POST http://localhost:$(MM_PORT_B)/api/v4/commands/execute \
		-H "Authorization: Bearer $$TOKEN_B" \
		-H "Content-Type: application/json" \
		-d '{"channel_id":"'"$$LTH_B"'","command":"/crossguard init-team outbound:high-to-low"}' >/dev/null && \
	echo "  Server B: init-team outbound:high-to-low" && \
	echo "Initializing channels..." && \
	curl -sf -X POST http://localhost:$(MM_PORT_A)/api/v4/commands/execute \
		-H "Authorization: Bearer $$TOKEN_A" \
		-H "Content-Type: application/json" \
		-d '{"channel_id":"'"$$LTH_A"'","command":"/crossguard init-channel outbound:low-to-high"}' >/dev/null && \
	echo "  Server A: low-to-high init-channel outbound:low-to-high" && \
	curl -sf -X POST http://localhost:$(MM_PORT_B)/api/v4/commands/execute \
		-H "Authorization: Bearer $$TOKEN_B" \
		-H "Content-Type: application/json" \
		-d '{"channel_id":"'"$$LTH_B"'","command":"/crossguard init-channel inbound:low-to-high"}' >/dev/null && \
	echo "  Server B: low-to-high init-channel inbound:low-to-high" && \
	curl -sf -X POST http://localhost:$(MM_PORT_A)/api/v4/commands/execute \
		-H "Authorization: Bearer $$TOKEN_A" \
		-H "Content-Type: application/json" \
		-d '{"channel_id":"'"$$BD_A"'","command":"/crossguard init-channel outbound:low-to-high"}' >/dev/null && \
	echo "  Server A: bi-directional init-channel outbound:low-to-high" && \
	curl -sf -X POST http://localhost:$(MM_PORT_A)/api/v4/commands/execute \
		-H "Authorization: Bearer $$TOKEN_A" \
		-H "Content-Type: application/json" \
		-d '{"channel_id":"'"$$BD_A"'","command":"/crossguard init-channel inbound:high-to-low"}' >/dev/null && \
	echo "  Server A: bi-directional init-channel inbound:high-to-low" && \
	curl -sf -X POST http://localhost:$(MM_PORT_B)/api/v4/commands/execute \
		-H "Authorization: Bearer $$TOKEN_B" \
		-H "Content-Type: application/json" \
		-d '{"channel_id":"'"$$BD_B"'","command":"/crossguard init-channel inbound:low-to-high"}' >/dev/null && \
	echo "  Server B: bi-directional init-channel inbound:low-to-high" && \
	curl -sf -X POST http://localhost:$(MM_PORT_B)/api/v4/commands/execute \
		-H "Authorization: Bearer $$TOKEN_B" \
		-H "Content-Type: application/json" \
		-d '{"channel_id":"'"$$BD_B"'","command":"/crossguard init-channel outbound:high-to-low"}' >/dev/null && \
	echo "  Server B: bi-directional init-channel outbound:high-to-low" && \
	echo "Posting smoke-test message from Server A..." && \
	TOKEN_USERA=$$(curl -sf -X POST http://localhost:$(MM_PORT_A)/api/v4/users/login \
		-d '{"login_id":"usera","password":"password"}' -i 2>/dev/null \
		| grep -i '^Token:' | awk '{print $$2}' | tr -d '\r') && \
	SMOKE_ID=$$(date +%s)-$$$$ && \
	curl -sf -X POST http://localhost:$(MM_PORT_A)/api/v4/posts \
		-H "Authorization: Bearer $$TOKEN_USERA" \
		-H "Content-Type: application/json" \
		-d '{"channel_id":"'"$$LTH_A"'","message":"smoke-test:'"$$SMOKE_ID"'"}' >/dev/null && \
	echo "  Posted smoke-test:$$SMOKE_ID to Server A low-to-high" && \
	echo "Waiting for relay..." && \
	sleep 3 && \
	FOUND=$$(curl -sf "http://localhost:$(MM_PORT_B)/api/v4/channels/$$LTH_B/posts?per_page=10" \
		-H "Authorization: Bearer $$TOKEN_B" | python3 -c "import sys,json;data=json.load(sys.stdin);sid='$$SMOKE_ID';found=any('smoke-test:'+sid in p.get('message','') for p in data.get('posts',{}).values());print('PASS' if found else 'FAIL');sys.exit(0 if found else 1)") && \
	echo "Smoke test result: $$FOUND" || \
	{ echo "Smoke test FAILED: message smoke-test:$$SMOKE_ID not found on Server B low-to-high"; exit 1; }

## Full integration test suite (loopback, file relay, XML, Azure)
.PHONY: docker-integration-test
docker-integration-test: docker-check
	@echo ""
	@echo "Running loopback rewrite-team test..."
	@TOKEN_A=$$(curl -sf -X POST http://localhost:$(MM_PORT_A)/api/v4/users/login \
		-d '{"login_id":"admin","password":"password"}' -i 2>/dev/null \
		| grep -i '^Token:' | awk '{print $$2}' | tr -d '\r') && \
	echo "Creating loop team on Server A..." && \
	$(DOCKER_COMPOSE) exec -T mattermost-a mmctl --local team create \
		--name loop \
		--display-name "Loop" 2>/dev/null || echo "  Team 'loop' already exists on Server A" && \
	$(DOCKER_COMPOSE) exec -T mattermost-a mmctl --local team users add loop admin 2>/dev/null || true && \
	$(DOCKER_COMPOSE) exec -T mattermost-a mmctl --local team users add loop usera 2>/dev/null || true && \
	echo "Getting team IDs..." && \
	LOOP_TEAM=$$(curl -sf http://localhost:$(MM_PORT_A)/api/v4/teams/name/loop \
		-H "Authorization: Bearer $$TOKEN_A" | python3 -c "import sys,json; print(json.load(sys.stdin)['id'])") && \
	TEST_TEAM=$$(curl -sf http://localhost:$(MM_PORT_A)/api/v4/teams/name/test \
		-H "Authorization: Bearer $$TOKEN_A" | python3 -c "import sys,json; print(json.load(sys.stdin)['id'])") && \
	echo "Creating local-loopback channels..." && \
	LOOP_CH=$$(curl -sf -X POST http://localhost:$(MM_PORT_A)/api/v4/channels \
		-H "Authorization: Bearer $$TOKEN_A" -H "Content-Type: application/json" \
		-d '{"team_id":"'"$$LOOP_TEAM"'","name":"local-loopback","display_name":"Local Loopback","type":"O"}' \
		2>/dev/null | python3 -c "import sys,json; print(json.load(sys.stdin)['id'])" 2>/dev/null || \
		curl -sf http://localhost:$(MM_PORT_A)/api/v4/teams/name/loop/channels/name/local-loopback \
		-H "Authorization: Bearer $$TOKEN_A" | python3 -c "import sys,json; print(json.load(sys.stdin)['id'])") && \
	echo "  Server A loop/local-loopback channel ($$LOOP_CH)" && \
	LB_CH=$$(curl -sf -X POST http://localhost:$(MM_PORT_A)/api/v4/channels \
		-H "Authorization: Bearer $$TOKEN_A" -H "Content-Type: application/json" \
		-d '{"team_id":"'"$$TEST_TEAM"'","name":"local-loopback","display_name":"Local Loopback","type":"O"}' \
		2>/dev/null | python3 -c "import sys,json; print(json.load(sys.stdin)['id'])" 2>/dev/null || \
		curl -sf http://localhost:$(MM_PORT_A)/api/v4/teams/name/test/channels/name/local-loopback \
		-H "Authorization: Bearer $$TOKEN_A" | python3 -c "import sys,json; print(json.load(sys.stdin)['id'])") && \
	echo "  Server A test/local-loopback channel ($$LB_CH)" && \
	echo "Adding users to channels..." && \
	USERA_ID=$$(curl -sf http://localhost:$(MM_PORT_A)/api/v4/users/username/usera \
		-H "Authorization: Bearer $$TOKEN_A" | python3 -c "import sys,json; print(json.load(sys.stdin)['id'])") && \
	USERAA_ID=$$(curl -sf http://localhost:$(MM_PORT_A)/api/v4/users/username/useraa \
		-H "Authorization: Bearer $$TOKEN_A" | python3 -c "import sys,json; print(json.load(sys.stdin)['id'])") && \
	curl -sf -X POST http://localhost:$(MM_PORT_A)/api/v4/channels/$$LOOP_CH/members \
		-H "Authorization: Bearer $$TOKEN_A" -H "Content-Type: application/json" \
		-d '{"user_id":"'"$$USERA_ID"'"}' >/dev/null 2>&1 || true && \
	curl -sf -X POST http://localhost:$(MM_PORT_A)/api/v4/channels/$$LB_CH/members \
		-H "Authorization: Bearer $$TOKEN_A" -H "Content-Type: application/json" \
		-d '{"user_id":"'"$$USERA_ID"'"}' >/dev/null 2>&1 || true && \
	echo "  usera added to both local-loopback channels" && \
	curl -sf -X POST http://localhost:$(MM_PORT_A)/api/v4/channels/$$LOOP_CH/members \
		-H "Authorization: Bearer $$TOKEN_A" -H "Content-Type: application/json" \
		-d '{"user_id":"'"$$USERAA_ID"'"}' >/dev/null 2>&1 || true && \
	curl -sf -X POST http://localhost:$(MM_PORT_A)/api/v4/channels/$$LB_CH/members \
		-H "Authorization: Bearer $$TOKEN_A" -H "Content-Type: application/json" \
		-d '{"user_id":"'"$$USERAA_ID"'"}' >/dev/null 2>&1 || true && \
	echo "  useraa added to both local-loopback channels" && \
	echo "Initializing loopback teams..." && \
	curl -sf -X POST http://localhost:$(MM_PORT_A)/api/v4/commands/execute \
		-H "Authorization: Bearer $$TOKEN_A" \
		-H "Content-Type: application/json" \
		-d '{"channel_id":"'"$$LOOP_CH"'","command":"/crossguard init-team outbound:loopback"}' >/dev/null && \
	echo "  Server A loop: init-team outbound:loopback" && \
	curl -sf -X POST http://localhost:$(MM_PORT_A)/api/v4/commands/execute \
		-H "Authorization: Bearer $$TOKEN_A" \
		-H "Content-Type: application/json" \
		-d '{"channel_id":"'"$$LB_CH"'","command":"/crossguard init-team inbound:loopback"}' >/dev/null && \
	echo "  Server A test: init-team inbound:loopback" && \
	echo "Initializing loopback channels..." && \
	curl -sf -X POST http://localhost:$(MM_PORT_A)/api/v4/commands/execute \
		-H "Authorization: Bearer $$TOKEN_A" \
		-H "Content-Type: application/json" \
		-d '{"channel_id":"'"$$LOOP_CH"'","command":"/crossguard init-channel outbound:loopback"}' >/dev/null && \
	echo "  Server A loop/local-loopback: init-channel outbound:loopback" && \
	curl -sf -X POST http://localhost:$(MM_PORT_A)/api/v4/commands/execute \
		-H "Authorization: Bearer $$TOKEN_A" \
		-H "Content-Type: application/json" \
		-d '{"channel_id":"'"$$LB_CH"'","command":"/crossguard init-channel inbound:loopback"}' >/dev/null && \
	echo "  Server A test/local-loopback: init-channel inbound:loopback" && \
	echo "Setting rewrite-team rule..." && \
	curl -sf -X POST http://localhost:$(MM_PORT_A)/api/v4/commands/execute \
		-H "Authorization: Bearer $$TOKEN_A" \
		-H "Content-Type: application/json" \
		-d '{"channel_id":"'"$$LB_CH"'","command":"/crossguard rewrite-team loopback loop"}' >/dev/null && \
	echo "  Server A: rewrite-team loopback loop -> test" && \
	echo "Posting loopback test message from Server A loop/local-loopback..." && \
	TOKEN_USERA=$$(curl -sf -X POST http://localhost:$(MM_PORT_A)/api/v4/users/login \
		-d '{"login_id":"usera","password":"password"}' -i 2>/dev/null \
		| grep -i '^Token:' | awk '{print $$2}' | tr -d '\r') && \
	LB_ID=$$(date +%s)-$$$$-lb && \
	curl -sf -X POST http://localhost:$(MM_PORT_A)/api/v4/posts \
		-H "Authorization: Bearer $$TOKEN_USERA" \
		-H "Content-Type: application/json" \
		-d '{"channel_id":"'"$$LOOP_CH"'","message":"loopback-test:'"$$LB_ID"'"}' >/dev/null && \
	echo "  Posted loopback-test:$$LB_ID to Server A loop/local-loopback" && \
	echo "Waiting for loopback relay..." && \
	sleep 3 && \
	LB_FOUND=$$(curl -sf "http://localhost:$(MM_PORT_A)/api/v4/channels/$$LB_CH/posts?per_page=10" \
		-H "Authorization: Bearer $$TOKEN_A" | python3 -c "import sys,json;data=json.load(sys.stdin);sid='$$LB_ID';found=any('loopback-test:'+sid in p.get('message','') for p in data.get('posts',{}).values());print('PASS' if found else 'FAIL');sys.exit(0 if found else 1)") && \
	echo "Loopback test result: $$LB_FOUND" || \
	{ echo "Loopback test FAILED: message loopback-test:$$LB_ID not found on Server A test/local-loopback"; exit 1; }
	@echo ""
	@echo "Running file attachment relay test..."
	@TOKEN_A=$$(curl -sf -X POST http://localhost:$(MM_PORT_A)/api/v4/users/login \
		-d '{"login_id":"admin","password":"password"}' -i 2>/dev/null \
		| grep -i '^Token:' | awk '{print $$2}' | tr -d '\r') && \
	TOKEN_B=$$(curl -sf -X POST http://localhost:$(MM_PORT_B)/api/v4/users/login \
		-d '{"login_id":"admin","password":"password"}' -i 2>/dev/null \
		| grep -i '^Token:' | awk '{print $$2}' | tr -d '\r') && \
	TOKEN_USERA=$$(curl -sf -X POST http://localhost:$(MM_PORT_A)/api/v4/users/login \
		-d '{"login_id":"usera","password":"password"}' -i 2>/dev/null \
		| grep -i '^Token:' | awk '{print $$2}' | tr -d '\r') && \
	LTH_A=$$(curl -sf http://localhost:$(MM_PORT_A)/api/v4/teams/name/test/channels/name/low-to-high \
		-H "Authorization: Bearer $$TOKEN_A" | python3 -c "import sys,json; print(json.load(sys.stdin)['id'])") && \
	LTH_B=$$(curl -sf http://localhost:$(MM_PORT_B)/api/v4/teams/name/test/channels/name/low-to-high \
		-H "Authorization: Bearer $$TOKEN_B" | python3 -c "import sys,json; print(json.load(sys.stdin)['id'])") && \
	FILE_ID=$$(date +%s)-$$$$ && \
	echo "Uploading sample.pdf to Server A and posting with message..." && \
	FILE_UPLOAD=$$(curl -sf -X POST "http://localhost:$(MM_PORT_A)/api/v4/files?channel_id=$$LTH_A" \
		-H "Authorization: Bearer $$TOKEN_USERA" \
		-F "files=@testdata/sample.pdf" | python3 -c "import sys,json; print(json.load(sys.stdin)['file_infos'][0]['id'])") && \
	POST_RESP=$$(curl -sf -X POST http://localhost:$(MM_PORT_A)/api/v4/posts \
		-H "Authorization: Bearer $$TOKEN_USERA" \
		-H "Content-Type: application/json" \
		-d '{"channel_id":"'"$$LTH_A"'","message":"file-test:'"$$FILE_ID"'","file_ids":["'"$$FILE_UPLOAD"'"]}') && \
	echo "  Posted file-test:$$FILE_ID with sample.pdf to Server A low-to-high" && \
	echo "Uploading sample.docx to Server A and posting with message..." && \
	DOCX_ID=$$(date +%s)-$$$$-docx && \
	DOCX_UPLOAD=$$(curl -sf -X POST "http://localhost:$(MM_PORT_A)/api/v4/files?channel_id=$$LTH_A" \
		-H "Authorization: Bearer $$TOKEN_USERA" \
		-F "files=@testdata/sample.docx" | python3 -c "import sys,json; print(json.load(sys.stdin)['file_infos'][0]['id'])") && \
	POST_RESP2=$$(curl -sf -X POST http://localhost:$(MM_PORT_A)/api/v4/posts \
		-H "Authorization: Bearer $$TOKEN_USERA" \
		-H "Content-Type: application/json" \
		-d '{"channel_id":"'"$$LTH_A"'","message":"file-test:'"$$DOCX_ID"'","file_ids":["'"$$DOCX_UPLOAD"'"]}') && \
	echo "  Posted file-test:$$DOCX_ID with sample.docx to Server A low-to-high" && \
	echo "Waiting for file relay (5s)..." && \
	sleep 5 && \
	echo "Verifying PDF relay on Server B..." && \
	PDF_FOUND=$$(curl -sf "http://localhost:$(MM_PORT_B)/api/v4/channels/$$LTH_B/posts?per_page=10" \
		-H "Authorization: Bearer $$TOKEN_B" | python3 -c "import sys,json;data=json.load(sys.stdin);sid='$$FILE_ID';found=any('file-test:'+sid in p.get('message','') and len(p.get('file_ids',[]))>0 for p in data.get('posts',{}).values());print('PASS' if found else 'FAIL');sys.exit(0 if found else 1)") && \
	echo "  PDF file relay test: $$PDF_FOUND" || \
	{ echo "  PDF file relay FAILED: file-test:$$FILE_ID not found with attachments on Server B"; exit 1; } && \
	echo "Verifying DOCX relay on Server B..." && \
	DOCX_FOUND=$$(curl -sf "http://localhost:$(MM_PORT_B)/api/v4/channels/$$LTH_B/posts?per_page=10" \
		-H "Authorization: Bearer $$TOKEN_B" | python3 -c "import sys,json;data=json.load(sys.stdin);sid='$$DOCX_ID';found=any('file-test:'+sid in p.get('message','') and len(p.get('file_ids',[]))>0 for p in data.get('posts',{}).values());print('PASS' if found else 'FAIL');sys.exit(0 if found else 1)") && \
	echo "  DOCX file relay test: $$DOCX_FOUND" || \
	{ echo "  DOCX file relay FAILED: file-test:$$DOCX_ID not found with attachments on Server B"; exit 1; }
	@echo ""
	@echo "Running XML loopback test..."
	@TOKEN_A=$$(curl -sf -X POST http://localhost:$(MM_PORT_A)/api/v4/users/login \
		-d '{"login_id":"admin","password":"password"}' -i 2>/dev/null \
		| grep -i '^Token:' | awk '{print $$2}' | tr -d '\r') && \
	echo "Getting team IDs..." && \
	LOOP_TEAM=$$(curl -sf http://localhost:$(MM_PORT_A)/api/v4/teams/name/loop \
		-H "Authorization: Bearer $$TOKEN_A" | python3 -c "import sys,json; print(json.load(sys.stdin)['id'])") && \
	TEST_TEAM=$$(curl -sf http://localhost:$(MM_PORT_A)/api/v4/teams/name/test \
		-H "Authorization: Bearer $$TOKEN_A" | python3 -c "import sys,json; print(json.load(sys.stdin)['id'])") && \
	echo "Creating xml-loopback channels..." && \
	XML_LOOP_CH=$$(curl -sf -X POST http://localhost:$(MM_PORT_A)/api/v4/channels \
		-H "Authorization: Bearer $$TOKEN_A" -H "Content-Type: application/json" \
		-d '{"team_id":"'"$$LOOP_TEAM"'","name":"xml-loopback","display_name":"XML Loopback","type":"O"}' \
		2>/dev/null | python3 -c "import sys,json; print(json.load(sys.stdin)['id'])" 2>/dev/null || \
		curl -sf http://localhost:$(MM_PORT_A)/api/v4/teams/name/loop/channels/name/xml-loopback \
		-H "Authorization: Bearer $$TOKEN_A" | python3 -c "import sys,json; print(json.load(sys.stdin)['id'])") && \
	echo "  Server A loop/xml-loopback channel ($$XML_LOOP_CH)" && \
	XML_LB_CH=$$(curl -sf -X POST http://localhost:$(MM_PORT_A)/api/v4/channels \
		-H "Authorization: Bearer $$TOKEN_A" -H "Content-Type: application/json" \
		-d '{"team_id":"'"$$TEST_TEAM"'","name":"xml-loopback","display_name":"XML Loopback","type":"O"}' \
		2>/dev/null | python3 -c "import sys,json; print(json.load(sys.stdin)['id'])" 2>/dev/null || \
		curl -sf http://localhost:$(MM_PORT_A)/api/v4/teams/name/test/channels/name/xml-loopback \
		-H "Authorization: Bearer $$TOKEN_A" | python3 -c "import sys,json; print(json.load(sys.stdin)['id'])") && \
	echo "  Server A test/xml-loopback channel ($$XML_LB_CH)" && \
	echo "Adding users to xml-loopback channels..." && \
	USERA_ID=$$(curl -sf http://localhost:$(MM_PORT_A)/api/v4/users/username/usera \
		-H "Authorization: Bearer $$TOKEN_A" | python3 -c "import sys,json; print(json.load(sys.stdin)['id'])") && \
	curl -sf -X POST http://localhost:$(MM_PORT_A)/api/v4/channels/$$XML_LOOP_CH/members \
		-H "Authorization: Bearer $$TOKEN_A" -H "Content-Type: application/json" \
		-d '{"user_id":"'"$$USERA_ID"'"}' >/dev/null 2>&1 || true && \
	curl -sf -X POST http://localhost:$(MM_PORT_A)/api/v4/channels/$$XML_LB_CH/members \
		-H "Authorization: Bearer $$TOKEN_A" -H "Content-Type: application/json" \
		-d '{"user_id":"'"$$USERA_ID"'"}' >/dev/null 2>&1 || true && \
	echo "  usera added to both xml-loopback channels" && \
	echo "Initializing xml-loopback teams..." && \
	curl -sf -X POST http://localhost:$(MM_PORT_A)/api/v4/commands/execute \
		-H "Authorization: Bearer $$TOKEN_A" \
		-H "Content-Type: application/json" \
		-d '{"channel_id":"'"$$XML_LOOP_CH"'","command":"/crossguard init-team outbound:xml-loopback"}' >/dev/null && \
	echo "  Server A loop: init-team outbound:xml-loopback" && \
	curl -sf -X POST http://localhost:$(MM_PORT_A)/api/v4/commands/execute \
		-H "Authorization: Bearer $$TOKEN_A" \
		-H "Content-Type: application/json" \
		-d '{"channel_id":"'"$$XML_LB_CH"'","command":"/crossguard init-team inbound:xml-loopback"}' >/dev/null && \
	echo "  Server A test: init-team inbound:xml-loopback" && \
	echo "Initializing xml-loopback channels..." && \
	curl -sf -X POST http://localhost:$(MM_PORT_A)/api/v4/commands/execute \
		-H "Authorization: Bearer $$TOKEN_A" \
		-H "Content-Type: application/json" \
		-d '{"channel_id":"'"$$XML_LOOP_CH"'","command":"/crossguard init-channel outbound:xml-loopback"}' >/dev/null && \
	echo "  Server A loop/xml-loopback: init-channel outbound:xml-loopback" && \
	curl -sf -X POST http://localhost:$(MM_PORT_A)/api/v4/commands/execute \
		-H "Authorization: Bearer $$TOKEN_A" \
		-H "Content-Type: application/json" \
		-d '{"channel_id":"'"$$XML_LB_CH"'","command":"/crossguard init-channel inbound:xml-loopback"}' >/dev/null && \
	echo "  Server A test/xml-loopback: init-channel inbound:xml-loopback" && \
	echo "Setting rewrite-team rule for xml-loopback..." && \
	curl -sf -X POST http://localhost:$(MM_PORT_A)/api/v4/commands/execute \
		-H "Authorization: Bearer $$TOKEN_A" \
		-H "Content-Type: application/json" \
		-d '{"channel_id":"'"$$XML_LB_CH"'","command":"/crossguard rewrite-team xml-loopback loop"}' >/dev/null && \
	echo "  Server A: rewrite-team xml-loopback loop -> test" && \
	echo "Posting XML loopback test message..." && \
	TOKEN_USERA=$$(curl -sf -X POST http://localhost:$(MM_PORT_A)/api/v4/users/login \
		-d '{"login_id":"usera","password":"password"}' -i 2>/dev/null \
		| grep -i '^Token:' | awk '{print $$2}' | tr -d '\r') && \
	XML_ID=$$(date +%s)-$$$$-xml && \
	curl -sf -X POST http://localhost:$(MM_PORT_A)/api/v4/posts \
		-H "Authorization: Bearer $$TOKEN_USERA" \
		-H "Content-Type: application/json" \
		-d '{"channel_id":"'"$$XML_LOOP_CH"'","message":"xml-smoke-test:'"$$XML_ID"'"}' >/dev/null && \
	echo "  Posted xml-smoke-test:$$XML_ID to Server A loop/xml-loopback" && \
	echo "Waiting for XML loopback relay..." && \
	sleep 3 && \
	XML_FOUND=$$(curl -sf "http://localhost:$(MM_PORT_A)/api/v4/channels/$$XML_LB_CH/posts?per_page=10" \
		-H "Authorization: Bearer $$TOKEN_A" | python3 -c "import sys,json;data=json.load(sys.stdin);sid='$$XML_ID';found=any('xml-smoke-test:'+sid in p.get('message','') for p in data.get('posts',{}).values());print('PASS' if found else 'FAIL');sys.exit(0 if found else 1)") && \
	echo "XML loopback test result: $$XML_FOUND" || \
	{ echo "XML loopback test FAILED: message xml-smoke-test:$$XML_ID not found on Server A test/xml-loopback"; exit 1; }
	@echo ""
	@echo "Running Azure integration tests..."
	@$(MAKE) docker-azure-smoke-test
	@$(MAKE) docker-azure-blob-smoke-test
	@$(MAKE) docker-servicebus-smoke-test

## Azure Queue Storage smoke test using Azurite (local emulator)
## Configures an Azure loopback on Server A: outbound -> azurite queue -> inbound
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
	@echo "Creating Azurite queue and blob container..."
	@$(DOCKER_COMPOSE) exec -T azurite sh -c 'apk add --quiet curl 2>/dev/null; exit 0'
	@TOKEN_A=$$(curl -sf -X POST http://localhost:$(MM_PORT_A)/api/v4/users/login \
		-d '{"login_id":"admin","password":"password"}' -i 2>/dev/null \
		| grep -i '^Token:' | awk '{print $$2}' | tr -d '\r') && \
	echo "Getting team IDs..." && \
	LOOP_TEAM=$$(curl -sf http://localhost:$(MM_PORT_A)/api/v4/teams/name/loop \
		-H "Authorization: Bearer $$TOKEN_A" | python3 -c "import sys,json; print(json.load(sys.stdin)['id'])") && \
	TEST_TEAM=$$(curl -sf http://localhost:$(MM_PORT_A)/api/v4/teams/name/test \
		-H "Authorization: Bearer $$TOKEN_A" | python3 -c "import sys,json; print(json.load(sys.stdin)['id'])") && \
	echo "Creating azure-loopback channels..." && \
	AZ_LOOP_CH=$$(curl -sf -X POST http://localhost:$(MM_PORT_A)/api/v4/channels \
		-H "Authorization: Bearer $$TOKEN_A" -H "Content-Type: application/json" \
		-d '{"team_id":"'"$$LOOP_TEAM"'","name":"azure-loopback","display_name":"Azure Queue Loopback","type":"O"}' \
		2>/dev/null | python3 -c "import sys,json; print(json.load(sys.stdin)['id'])" 2>/dev/null || \
		curl -sf http://localhost:$(MM_PORT_A)/api/v4/teams/name/loop/channels/name/azure-loopback \
		-H "Authorization: Bearer $$TOKEN_A" | python3 -c "import sys,json; print(json.load(sys.stdin)['id'])") && \
	echo "  Server A loop/azure-loopback channel ($$AZ_LOOP_CH)" && \
	AZ_LB_CH=$$(curl -sf -X POST http://localhost:$(MM_PORT_A)/api/v4/channels \
		-H "Authorization: Bearer $$TOKEN_A" -H "Content-Type: application/json" \
		-d '{"team_id":"'"$$TEST_TEAM"'","name":"azure-loopback","display_name":"Azure Queue Loopback","type":"O"}' \
		2>/dev/null | python3 -c "import sys,json; print(json.load(sys.stdin)['id'])" 2>/dev/null || \
		curl -sf http://localhost:$(MM_PORT_A)/api/v4/teams/name/test/channels/name/azure-loopback \
		-H "Authorization: Bearer $$TOKEN_A" | python3 -c "import sys,json; print(json.load(sys.stdin)['id'])") && \
	echo "  Server A test/azure-loopback channel ($$AZ_LB_CH)" && \
	echo "Adding users to azure-loopback channels..." && \
	USERA_ID=$$(curl -sf http://localhost:$(MM_PORT_A)/api/v4/users/username/usera \
		-H "Authorization: Bearer $$TOKEN_A" | python3 -c "import sys,json; print(json.load(sys.stdin)['id'])") && \
	curl -sf -X POST http://localhost:$(MM_PORT_A)/api/v4/channels/$$AZ_LOOP_CH/members \
		-H "Authorization: Bearer $$TOKEN_A" -H "Content-Type: application/json" \
		-d '{"user_id":"'"$$USERA_ID"'"}' >/dev/null 2>&1 || true && \
	curl -sf -X POST http://localhost:$(MM_PORT_A)/api/v4/channels/$$AZ_LB_CH/members \
		-H "Authorization: Bearer $$TOKEN_A" -H "Content-Type: application/json" \
		-d '{"user_id":"'"$$USERA_ID"'"}' >/dev/null 2>&1 || true && \
	echo "  usera added to both azure-loopback channels" && \
	echo "Adding Azure loopback connections to Server A config..." && \
	EXISTING=$$(curl -sf http://localhost:$(MM_PORT_A)/api/v4/config \
		-H "Authorization: Bearer $$TOKEN_A" | python3 -c "import sys,json; c=json.load(sys.stdin); ps=c.get('PluginSettings',{}).get('Plugins',{}).get('crossguard',{}); print(ps.get('outboundconnections','[]'))") && \
	NEW_OB=$$(python3 -c "import sys,json; existing=json.loads('$$EXISTING'); existing.append({\"name\":\"azure-loopback\",\"provider\":\"azure-queue\",\"message_format\":\"xml\",\"file_transfer_enabled\":True,\"azure_queue\":{\"queue_service_url\":\"$(AZURITE_QUEUE_URL)\",\"blob_service_url\":\"$(AZURITE_BLOB_URL)\",\"account_name\":\"$(AZURITE_ACCOUNT_NAME)\",\"account_key\":\"$(AZURITE_ACCOUNT_KEY)\",\"queue_name\":\"$(AZURITE_QUEUE)\",\"blob_container_name\":\"$(AZURITE_BLOB)\",\"poll_interval_seconds\":1,\"blob_poll_interval_seconds\":1}}); print(json.dumps(existing))") && \
	EXISTING_IB=$$(curl -sf http://localhost:$(MM_PORT_A)/api/v4/config \
		-H "Authorization: Bearer $$TOKEN_A" | python3 -c "import sys,json; c=json.load(sys.stdin); ps=c.get('PluginSettings',{}).get('Plugins',{}).get('crossguard',{}); print(ps.get('inboundconnections','[]'))") && \
	NEW_IB=$$(python3 -c "import sys,json; existing=json.loads('$$EXISTING_IB'); existing.append({\"name\":\"azure-loopback\",\"provider\":\"azure-queue\",\"message_format\":\"xml\",\"file_transfer_enabled\":True,\"azure_queue\":{\"queue_service_url\":\"$(AZURITE_QUEUE_URL)\",\"blob_service_url\":\"$(AZURITE_BLOB_URL)\",\"account_name\":\"$(AZURITE_ACCOUNT_NAME)\",\"account_key\":\"$(AZURITE_ACCOUNT_KEY)\",\"queue_name\":\"$(AZURITE_QUEUE)\",\"blob_container_name\":\"$(AZURITE_BLOB)\",\"poll_interval_seconds\":1,\"blob_poll_interval_seconds\":1}}); print(json.dumps(existing))") && \
	curl -sf -X PUT http://localhost:$(MM_PORT_A)/api/v4/config/patch \
		-H "Authorization: Bearer $$TOKEN_A" \
		-H "Content-Type: application/json" \
		-d '{"PluginSettings":{"Plugins":{"crossguard":{"outboundconnections":"'"$$(echo $$NEW_OB | sed 's/"/\\"/g')"'","inboundconnections":"'"$$(echo $$NEW_IB | sed 's/"/\\"/g')"'"}}}}' >/dev/null && \
	echo "Server A configured with Azure loopback connection (outbound + inbound)" && \
	echo "Resetting plugin to pick up new config..." && \
	$(DOCKER_COMPOSE) exec -T mattermost-a mmctl --local plugin disable $(PLUGIN_ID) && \
	$(DOCKER_COMPOSE) exec -T mattermost-a mmctl --local plugin enable $(PLUGIN_ID) && \
	sleep 2 && \
	echo "Initializing Azure loopback teams..." && \
	curl -sf -X POST http://localhost:$(MM_PORT_A)/api/v4/commands/execute \
		-H "Authorization: Bearer $$TOKEN_A" \
		-H "Content-Type: application/json" \
		-d '{"channel_id":"'"$$AZ_LOOP_CH"'","command":"/crossguard init-team outbound:azure-loopback"}' >/dev/null && \
	echo "  Server A loop: init-team outbound:azure-loopback" && \
	curl -sf -X POST http://localhost:$(MM_PORT_A)/api/v4/commands/execute \
		-H "Authorization: Bearer $$TOKEN_A" \
		-H "Content-Type: application/json" \
		-d '{"channel_id":"'"$$AZ_LB_CH"'","command":"/crossguard init-team inbound:azure-loopback"}' >/dev/null && \
	echo "  Server A test: init-team inbound:azure-loopback" && \
	curl -sf -X POST http://localhost:$(MM_PORT_A)/api/v4/commands/execute \
		-H "Authorization: Bearer $$TOKEN_A" \
		-H "Content-Type: application/json" \
		-d '{"channel_id":"'"$$AZ_LOOP_CH"'","command":"/crossguard init-channel outbound:azure-loopback"}' >/dev/null && \
	echo "  Server A loop/azure-loopback: init-channel outbound:azure-loopback" && \
	curl -sf -X POST http://localhost:$(MM_PORT_A)/api/v4/commands/execute \
		-H "Authorization: Bearer $$TOKEN_A" \
		-H "Content-Type: application/json" \
		-d '{"channel_id":"'"$$AZ_LB_CH"'","command":"/crossguard init-channel inbound:azure-loopback"}' >/dev/null && \
	echo "  Server A test/azure-loopback: init-channel inbound:azure-loopback" && \
	echo "Setting rewrite-team rule..." && \
	curl -sf -X POST http://localhost:$(MM_PORT_A)/api/v4/commands/execute \
		-H "Authorization: Bearer $$TOKEN_A" \
		-H "Content-Type: application/json" \
		-d '{"channel_id":"'"$$AZ_LB_CH"'","command":"/crossguard rewrite-team azure-loopback loop"}' >/dev/null && \
	echo "  Server A: rewrite-team azure-loopback loop -> test" && \
	echo "Posting Azure smoke-test message from Server A loop/azure-loopback..." && \
	TOKEN_USERA=$$(curl -sf -X POST http://localhost:$(MM_PORT_A)/api/v4/users/login \
		-d '{"login_id":"usera","password":"password"}' -i 2>/dev/null \
		| grep -i '^Token:' | awk '{print $$2}' | tr -d '\r') && \
	AZ_ID=$$(date +%s)-$$$$-az && \
	curl -sf -X POST http://localhost:$(MM_PORT_A)/api/v4/posts \
		-H "Authorization: Bearer $$TOKEN_USERA" \
		-H "Content-Type: application/json" \
		-d '{"channel_id":"'"$$AZ_LOOP_CH"'","message":"azure-smoke-test:'"$$AZ_ID"'"}' >/dev/null && \
	echo "  Posted azure-smoke-test:$$AZ_ID to Server A loop/azure-loopback" && \
	echo "Waiting for Azure relay (3s)..." && \
	sleep 3 && \
	AZ_FOUND=$$(curl -sf "http://localhost:$(MM_PORT_A)/api/v4/channels/$$AZ_LB_CH/posts?per_page=10" \
		-H "Authorization: Bearer $$TOKEN_A" | python3 -c "import sys,json;data=json.load(sys.stdin);sid='$$AZ_ID';found=any('azure-smoke-test:'+sid in p.get('message','') for p in data.get('posts',{}).values());print('PASS' if found else 'FAIL');sys.exit(0 if found else 1)") && \
	echo "Azure message relay test: $$AZ_FOUND" || \
	{ echo "Azure message relay FAILED: azure-smoke-test:$$AZ_ID not found on Server A test/azure-loopback"; exit 1; }
	@echo ""
	@echo "Running Azure file relay test..."
	@TOKEN_A=$$(curl -sf -X POST http://localhost:$(MM_PORT_A)/api/v4/users/login \
		-d '{"login_id":"admin","password":"password"}' -i 2>/dev/null \
		| grep -i '^Token:' | awk '{print $$2}' | tr -d '\r') && \
	TOKEN_USERA=$$(curl -sf -X POST http://localhost:$(MM_PORT_A)/api/v4/users/login \
		-d '{"login_id":"usera","password":"password"}' -i 2>/dev/null \
		| grep -i '^Token:' | awk '{print $$2}' | tr -d '\r') && \
	AZ_LOOP_CH=$$(curl -sf http://localhost:$(MM_PORT_A)/api/v4/teams/name/loop/channels/name/azure-loopback \
		-H "Authorization: Bearer $$TOKEN_A" | python3 -c "import sys,json; print(json.load(sys.stdin)['id'])") && \
	AZ_LB_CH=$$(curl -sf http://localhost:$(MM_PORT_A)/api/v4/teams/name/test/channels/name/azure-loopback \
		-H "Authorization: Bearer $$TOKEN_A" | python3 -c "import sys,json; print(json.load(sys.stdin)['id'])") && \
	AZ_FILE_ID=$$(date +%s)-$$$$-azf && \
	echo "Uploading sample.pdf to Server A and posting via Azure connection..." && \
	FILE_UPLOAD=$$(curl -sf -X POST "http://localhost:$(MM_PORT_A)/api/v4/files?channel_id=$$AZ_LOOP_CH" \
		-H "Authorization: Bearer $$TOKEN_USERA" \
		-F "files=@testdata/sample.pdf" | python3 -c "import sys,json; print(json.load(sys.stdin)['file_infos'][0]['id'])") && \
	curl -sf -X POST http://localhost:$(MM_PORT_A)/api/v4/posts \
		-H "Authorization: Bearer $$TOKEN_USERA" \
		-H "Content-Type: application/json" \
		-d '{"channel_id":"'"$$AZ_LOOP_CH"'","message":"azure-file-test:'"$$AZ_FILE_ID"'","file_ids":["'"$$FILE_UPLOAD"'"]}' >/dev/null && \
	echo "  Posted azure-file-test:$$AZ_FILE_ID with sample.pdf to Server A loop/azure-loopback" && \
	echo "Waiting for Azure file relay (4s)..." && \
	sleep 4 && \
	AZ_FILE_FOUND=$$(curl -sf "http://localhost:$(MM_PORT_A)/api/v4/channels/$$AZ_LB_CH/posts?per_page=10" \
		-H "Authorization: Bearer $$TOKEN_A" | python3 -c "import sys,json;data=json.load(sys.stdin);sid='$$AZ_FILE_ID';found=any('azure-file-test:'+sid in p.get('message','') and len(p.get('file_ids',[]))>0 for p in data.get('posts',{}).values());print('PASS' if found else 'FAIL');sys.exit(0 if found else 1)") && \
	echo "Azure file relay test: $$AZ_FILE_FOUND" || \
	{ echo "Azure file relay FAILED: azure-file-test:$$AZ_FILE_ID not found with attachments on Server A test/azure-loopback"; exit 1; }

## Azure Blob Storage (batched) smoke test using Azurite (local emulator).
## Configures an azure-blob loopback on Server A: outbound WAL batch -> azurite blob -> inbound poll.
.PHONY: docker-azure-blob-smoke-test
docker-azure-blob-smoke-test: docker-check
	@echo ""
	@echo "Running Azure Blob (batched) smoke test..."
	@echo "Ensuring loop team exists on Server A..."
	@$(DOCKER_COMPOSE) exec -T mattermost-a mmctl --local team create \
		--name loop \
		--display-name "Loop" 2>/dev/null || echo "  Team 'loop' already exists on Server A"
	@$(DOCKER_COMPOSE) exec -T mattermost-a mmctl --local team users add loop admin 2>/dev/null || true
	@$(DOCKER_COMPOSE) exec -T mattermost-a mmctl --local team users add loop usera 2>/dev/null || true
	@TOKEN_A=$$(curl -sf -X POST http://localhost:$(MM_PORT_A)/api/v4/users/login \
		-d '{"login_id":"admin","password":"password"}' -i 2>/dev/null \
		| grep -i '^Token:' | awk '{print $$2}' | tr -d '\r') && \
	echo "Getting team IDs..." && \
	LOOP_TEAM=$$(curl -sf http://localhost:$(MM_PORT_A)/api/v4/teams/name/loop \
		-H "Authorization: Bearer $$TOKEN_A" | python3 -c "import sys,json; print(json.load(sys.stdin)['id'])") && \
	if [ -z "$$LOOP_TEAM" ]; then echo "FAILED: could not get loop team id"; exit 1; fi && \
	TEST_TEAM=$$(curl -sf http://localhost:$(MM_PORT_A)/api/v4/teams/name/test \
		-H "Authorization: Bearer $$TOKEN_A" | python3 -c "import sys,json; print(json.load(sys.stdin)['id'])") && \
	echo "Creating azure-blob-loopback channels..." && \
	AZB_LOOP_CH=$$(curl -sf -X POST http://localhost:$(MM_PORT_A)/api/v4/channels \
		-H "Authorization: Bearer $$TOKEN_A" -H "Content-Type: application/json" \
		-d '{"team_id":"'"$$LOOP_TEAM"'","name":"azure-blob-loopback","display_name":"Azure Blob Loopback","type":"O"}' \
		2>/dev/null | python3 -c "import sys,json; print(json.load(sys.stdin)['id'])" 2>/dev/null || \
		curl -sf http://localhost:$(MM_PORT_A)/api/v4/teams/name/loop/channels/name/azure-blob-loopback \
		-H "Authorization: Bearer $$TOKEN_A" | python3 -c "import sys,json; print(json.load(sys.stdin)['id'])") && \
	echo "  Server A loop/azure-blob-loopback channel ($$AZB_LOOP_CH)" && \
	AZB_LB_CH=$$(curl -sf -X POST http://localhost:$(MM_PORT_A)/api/v4/channels \
		-H "Authorization: Bearer $$TOKEN_A" -H "Content-Type: application/json" \
		-d '{"team_id":"'"$$TEST_TEAM"'","name":"azure-blob-loopback","display_name":"Azure Blob Loopback","type":"O"}' \
		2>/dev/null | python3 -c "import sys,json; print(json.load(sys.stdin)['id'])" 2>/dev/null || \
		curl -sf http://localhost:$(MM_PORT_A)/api/v4/teams/name/test/channels/name/azure-blob-loopback \
		-H "Authorization: Bearer $$TOKEN_A" | python3 -c "import sys,json; print(json.load(sys.stdin)['id'])") && \
	echo "  Server A test/azure-blob-loopback channel ($$AZB_LB_CH)" && \
	echo "Adding users to azure-blob-loopback channels..." && \
	USERA_ID=$$(curl -sf http://localhost:$(MM_PORT_A)/api/v4/users/username/usera \
		-H "Authorization: Bearer $$TOKEN_A" | python3 -c "import sys,json; print(json.load(sys.stdin)['id'])") && \
	curl -sf -X POST http://localhost:$(MM_PORT_A)/api/v4/channels/$$AZB_LOOP_CH/members \
		-H "Authorization: Bearer $$TOKEN_A" -H "Content-Type: application/json" \
		-d '{"user_id":"'"$$USERA_ID"'"}' >/dev/null 2>&1 || true && \
	curl -sf -X POST http://localhost:$(MM_PORT_A)/api/v4/channels/$$AZB_LB_CH/members \
		-H "Authorization: Bearer $$TOKEN_A" -H "Content-Type: application/json" \
		-d '{"user_id":"'"$$USERA_ID"'"}' >/dev/null 2>&1 || true && \
	echo "  usera added to both azure-blob-loopback channels" && \
	echo "Adding azure-blob loopback connections to Server A config..." && \
	EXISTING=$$(curl -sf http://localhost:$(MM_PORT_A)/api/v4/config \
		-H "Authorization: Bearer $$TOKEN_A" | python3 -c "import sys,json; c=json.load(sys.stdin); ps=c.get('PluginSettings',{}).get('Plugins',{}).get('crossguard',{}); print(ps.get('outboundconnections','[]'))") && \
	NEW_OB=$$(python3 -c "import sys,json; existing=json.loads('$$EXISTING'); existing=[c for c in existing if c.get('name')!='azure-blob-loopback']; existing.append({\"name\":\"azure-blob-loopback\",\"provider\":\"azure-blob\",\"message_format\":\"json\",\"file_transfer_enabled\":True,\"azure_blob\":{\"service_url\":\"$(AZURITE_BLOB_URL)\",\"account_name\":\"$(AZURITE_ACCOUNT_NAME)\",\"account_key\":\"$(AZURITE_ACCOUNT_KEY)\",\"blob_container_name\":\"$(AZURITE_BLOB_BATCH)\",\"flush_interval_seconds\":1,\"batch_poll_interval_seconds\":1}}); print(json.dumps(existing))") && \
	EXISTING_IB=$$(curl -sf http://localhost:$(MM_PORT_A)/api/v4/config \
		-H "Authorization: Bearer $$TOKEN_A" | python3 -c "import sys,json; c=json.load(sys.stdin); ps=c.get('PluginSettings',{}).get('Plugins',{}).get('crossguard',{}); print(ps.get('inboundconnections','[]'))") && \
	NEW_IB=$$(python3 -c "import sys,json; existing=json.loads('$$EXISTING_IB'); existing=[c for c in existing if c.get('name')!='azure-blob-loopback']; existing.append({\"name\":\"azure-blob-loopback\",\"provider\":\"azure-blob\",\"message_format\":\"json\",\"file_transfer_enabled\":True,\"azure_blob\":{\"service_url\":\"$(AZURITE_BLOB_URL)\",\"account_name\":\"$(AZURITE_ACCOUNT_NAME)\",\"account_key\":\"$(AZURITE_ACCOUNT_KEY)\",\"blob_container_name\":\"$(AZURITE_BLOB_BATCH)\",\"flush_interval_seconds\":1,\"batch_poll_interval_seconds\":1}}); print(json.dumps(existing))") && \
	curl -sf -X PUT http://localhost:$(MM_PORT_A)/api/v4/config/patch \
		-H "Authorization: Bearer $$TOKEN_A" \
		-H "Content-Type: application/json" \
		-d '{"PluginSettings":{"Plugins":{"crossguard":{"outboundconnections":"'"$$(echo $$NEW_OB | sed 's/"/\\"/g')"'","inboundconnections":"'"$$(echo $$NEW_IB | sed 's/"/\\"/g')"'"}}}}' >/dev/null && \
	echo "Server A configured with azure-blob loopback connection (outbound + inbound, flush=1s, poll=1s)" && \
	echo "Resetting plugin to pick up new config..." && \
	$(DOCKER_COMPOSE) exec -T mattermost-a mmctl --local plugin disable $(PLUGIN_ID) && \
	$(DOCKER_COMPOSE) exec -T mattermost-a mmctl --local plugin enable $(PLUGIN_ID) && \
	sleep 2 && \
	echo "Initializing azure-blob loopback teams..." && \
	curl -sf -X POST http://localhost:$(MM_PORT_A)/api/v4/commands/execute \
		-H "Authorization: Bearer $$TOKEN_A" \
		-H "Content-Type: application/json" \
		-d '{"channel_id":"'"$$AZB_LOOP_CH"'","command":"/crossguard init-team outbound:azure-blob-loopback"}' >/dev/null && \
	echo "  Server A loop: init-team outbound:azure-blob-loopback" && \
	curl -sf -X POST http://localhost:$(MM_PORT_A)/api/v4/commands/execute \
		-H "Authorization: Bearer $$TOKEN_A" \
		-H "Content-Type: application/json" \
		-d '{"channel_id":"'"$$AZB_LB_CH"'","command":"/crossguard init-team inbound:azure-blob-loopback"}' >/dev/null && \
	echo "  Server A test: init-team inbound:azure-blob-loopback" && \
	curl -sf -X POST http://localhost:$(MM_PORT_A)/api/v4/commands/execute \
		-H "Authorization: Bearer $$TOKEN_A" \
		-H "Content-Type: application/json" \
		-d '{"channel_id":"'"$$AZB_LOOP_CH"'","command":"/crossguard init-channel outbound:azure-blob-loopback"}' >/dev/null && \
	echo "  Server A loop/azure-blob-loopback: init-channel outbound:azure-blob-loopback" && \
	curl -sf -X POST http://localhost:$(MM_PORT_A)/api/v4/commands/execute \
		-H "Authorization: Bearer $$TOKEN_A" \
		-H "Content-Type: application/json" \
		-d '{"channel_id":"'"$$AZB_LB_CH"'","command":"/crossguard init-channel inbound:azure-blob-loopback"}' >/dev/null && \
	echo "  Server A test/azure-blob-loopback: init-channel inbound:azure-blob-loopback" && \
	echo "Setting rewrite-team rule..." && \
	curl -sf -X POST http://localhost:$(MM_PORT_A)/api/v4/commands/execute \
		-H "Authorization: Bearer $$TOKEN_A" \
		-H "Content-Type: application/json" \
		-d '{"channel_id":"'"$$AZB_LB_CH"'","command":"/crossguard rewrite-team azure-blob-loopback loop"}' >/dev/null && \
	echo "  Server A: rewrite-team azure-blob-loopback loop -> test" && \
	echo "Posting azure-blob smoke-test message from Server A loop/azure-blob-loopback..." && \
	TOKEN_USERA=$$(curl -sf -X POST http://localhost:$(MM_PORT_A)/api/v4/users/login \
		-d '{"login_id":"usera","password":"password"}' -i 2>/dev/null \
		| grep -i '^Token:' | awk '{print $$2}' | tr -d '\r') && \
	AZB_ID=$$(date +%s)-$$$$-azb && \
	curl -sf -X POST http://localhost:$(MM_PORT_A)/api/v4/posts \
		-H "Authorization: Bearer $$TOKEN_USERA" \
		-H "Content-Type: application/json" \
		-d '{"channel_id":"'"$$AZB_LOOP_CH"'","message":"azure-blob-smoke-test:'"$$AZB_ID"'"}' >/dev/null && \
	echo "  Posted azure-blob-smoke-test:$$AZB_ID to Server A loop/azure-blob-loopback" && \
	echo "Waiting for azure-blob relay (6s: 1s flush + 2s inbound poll + 3s buffer)..." && \
	sleep 6 && \
	AZB_FOUND=$$(curl -sf "http://localhost:$(MM_PORT_A)/api/v4/channels/$$AZB_LB_CH/posts?per_page=10" \
		-H "Authorization: Bearer $$TOKEN_A" | python3 -c "import sys,json;data=json.load(sys.stdin);sid='$$AZB_ID';found=any('azure-blob-smoke-test:'+sid in p.get('message','') for p in data.get('posts',{}).values());print('PASS' if found else 'FAIL');sys.exit(0 if found else 1)") && \
	echo "azure-blob message relay test: $$AZB_FOUND" || \
	{ echo "azure-blob message relay FAILED: azure-blob-smoke-test:$$AZB_ID not found on Server A test/azure-blob-loopback"; exit 1; }
	@echo ""
	@echo "Running azure-blob deferred file relay test..."
	@TOKEN_A=$$(curl -sf -X POST http://localhost:$(MM_PORT_A)/api/v4/users/login \
		-d '{"login_id":"admin","password":"password"}' -i 2>/dev/null \
		| grep -i '^Token:' | awk '{print $$2}' | tr -d '\r') && \
	TOKEN_USERA=$$(curl -sf -X POST http://localhost:$(MM_PORT_A)/api/v4/users/login \
		-d '{"login_id":"usera","password":"password"}' -i 2>/dev/null \
		| grep -i '^Token:' | awk '{print $$2}' | tr -d '\r') && \
	AZB_LOOP_CH=$$(curl -sf http://localhost:$(MM_PORT_A)/api/v4/teams/name/loop/channels/name/azure-blob-loopback \
		-H "Authorization: Bearer $$TOKEN_A" | python3 -c "import sys,json; print(json.load(sys.stdin)['id'])") && \
	AZB_LB_CH=$$(curl -sf http://localhost:$(MM_PORT_A)/api/v4/teams/name/test/channels/name/azure-blob-loopback \
		-H "Authorization: Bearer $$TOKEN_A" | python3 -c "import sys,json; print(json.load(sys.stdin)['id'])") && \
	AZB_FILE_ID=$$(date +%s)-$$$$-azbf && \
	echo "Uploading sample.pdf to Server A and posting via azure-blob connection..." && \
	FILE_UPLOAD=$$(curl -sf -X POST "http://localhost:$(MM_PORT_A)/api/v4/files?channel_id=$$AZB_LOOP_CH" \
		-H "Authorization: Bearer $$TOKEN_USERA" \
		-F "files=@testdata/sample.pdf" | python3 -c "import sys,json; print(json.load(sys.stdin)['file_infos'][0]['id'])") && \
	curl -sf -X POST http://localhost:$(MM_PORT_A)/api/v4/posts \
		-H "Authorization: Bearer $$TOKEN_USERA" \
		-H "Content-Type: application/json" \
		-d '{"channel_id":"'"$$AZB_LOOP_CH"'","message":"azure-blob-file-test:'"$$AZB_FILE_ID"'","file_ids":["'"$$FILE_UPLOAD"'"]}' >/dev/null && \
	echo "  Posted azure-blob-file-test:$$AZB_FILE_ID with sample.pdf to Server A loop/azure-blob-loopback" && \
	echo "Waiting for azure-blob file relay (10s: 1s flush + 2s msg poll + 2s file watch + 5s buffer)..." && \
	sleep 10 && \
	AZB_FILE_FOUND=$$(curl -sf "http://localhost:$(MM_PORT_A)/api/v4/channels/$$AZB_LB_CH/posts?per_page=10" \
		-H "Authorization: Bearer $$TOKEN_A" | python3 -c "import sys,json;data=json.load(sys.stdin);sid='$$AZB_FILE_ID';found=any('azure-blob-file-test:'+sid in p.get('message','') and len(p.get('file_ids',[]))>0 for p in data.get('posts',{}).values());print('PASS' if found else 'FAIL');sys.exit(0 if found else 1)") && \
	echo "azure-blob deferred file relay test: $$AZB_FILE_FOUND" || \
	{ echo "azure-blob file relay FAILED: azure-blob-file-test:$$AZB_FILE_ID not found with attachments on Server A test/azure-blob-loopback"; exit 1; }


## Azure Service Bus smoke test using the local Service Bus emulator.
## Readiness is gated by servicebus-probe (AMQP PeekMessages) rather than
## docker healthcheck because the emulator binds 5672 before its SQL schema
## is ready.

SERVICEBUS_PORT_DEFAULT := 5672
SERVICEBUS_QUEUE := crossguard-relay
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
	@echo "Running Azure Service Bus smoke test..."
	@echo "Ensuring loop team exists on Server A..."
	@$(DOCKER_COMPOSE) exec -T mattermost-a mmctl --local team create \
		--name loop \
		--display-name "Loop" 2>/dev/null || echo "  Team 'loop' already exists on Server A"
	@$(DOCKER_COMPOSE) exec -T mattermost-a mmctl --local team users add loop admin 2>/dev/null || true
	@$(DOCKER_COMPOSE) exec -T mattermost-a mmctl --local team users add loop usera 2>/dev/null || true
	@TOKEN_A=$$(curl -sf -X POST http://localhost:$(MM_PORT_A)/api/v4/users/login \
		-d '{"login_id":"admin","password":"password"}' -i 2>/dev/null \
		| grep -i '^Token:' | awk '{print $$2}' | tr -d '\r') && \
	echo "Getting team IDs..." && \
	LOOP_TEAM=$$(curl -sf http://localhost:$(MM_PORT_A)/api/v4/teams/name/loop \
		-H "Authorization: Bearer $$TOKEN_A" | python3 -c "import sys,json; print(json.load(sys.stdin)['id'])") && \
	if [ -z "$$LOOP_TEAM" ]; then echo "FAILED: could not get loop team id"; exit 1; fi && \
	TEST_TEAM=$$(curl -sf http://localhost:$(MM_PORT_A)/api/v4/teams/name/test \
		-H "Authorization: Bearer $$TOKEN_A" | python3 -c "import sys,json; print(json.load(sys.stdin)['id'])") && \
	echo "Creating servicebus-loopback channels on Server A..." && \
	SB_LOOP_CH=$$(curl -sf -X POST http://localhost:$(MM_PORT_A)/api/v4/channels \
		-H "Authorization: Bearer $$TOKEN_A" -H "Content-Type: application/json" \
		-d '{"team_id":"'"$$LOOP_TEAM"'","name":"servicebus-loopback","display_name":"Service Bus Loopback","type":"O"}' \
		2>/dev/null | python3 -c "import sys,json; print(json.load(sys.stdin)['id'])" 2>/dev/null || \
		curl -sf http://localhost:$(MM_PORT_A)/api/v4/teams/name/loop/channels/name/servicebus-loopback \
		-H "Authorization: Bearer $$TOKEN_A" | python3 -c "import sys,json; print(json.load(sys.stdin)['id'])") && \
	echo "  Server A loop/servicebus-loopback channel ($$SB_LOOP_CH)" && \
	SB_LB_CH=$$(curl -sf -X POST http://localhost:$(MM_PORT_A)/api/v4/channels \
		-H "Authorization: Bearer $$TOKEN_A" -H "Content-Type: application/json" \
		-d '{"team_id":"'"$$TEST_TEAM"'","name":"servicebus-loopback","display_name":"Service Bus Loopback","type":"O"}' \
		2>/dev/null | python3 -c "import sys,json; print(json.load(sys.stdin)['id'])" 2>/dev/null || \
		curl -sf http://localhost:$(MM_PORT_A)/api/v4/teams/name/test/channels/name/servicebus-loopback \
		-H "Authorization: Bearer $$TOKEN_A" | python3 -c "import sys,json; print(json.load(sys.stdin)['id'])") && \
	echo "  Server A test/servicebus-loopback channel ($$SB_LB_CH)" && \
	USERA_ID=$$(curl -sf http://localhost:$(MM_PORT_A)/api/v4/users/username/usera \
		-H "Authorization: Bearer $$TOKEN_A" | python3 -c "import sys,json; print(json.load(sys.stdin)['id'])") && \
	curl -sf -X POST http://localhost:$(MM_PORT_A)/api/v4/channels/$$SB_LOOP_CH/members \
		-H "Authorization: Bearer $$TOKEN_A" -H "Content-Type: application/json" \
		-d '{"user_id":"'"$$USERA_ID"'"}' >/dev/null 2>&1 || true && \
	curl -sf -X POST http://localhost:$(MM_PORT_A)/api/v4/channels/$$SB_LB_CH/members \
		-H "Authorization: Bearer $$TOKEN_A" -H "Content-Type: application/json" \
		-d '{"user_id":"'"$$USERA_ID"'"}' >/dev/null 2>&1 || true && \
	echo "  usera added to both servicebus-loopback channels" && \
	echo "Adding azure-servicebus loopback connection to Server A config..." && \
	EXISTING=$$(curl -sf http://localhost:$(MM_PORT_A)/api/v4/config \
		-H "Authorization: Bearer $$TOKEN_A" | python3 -c "import sys,json; c=json.load(sys.stdin); ps=c.get('PluginSettings',{}).get('Plugins',{}).get('crossguard',{}); print(ps.get('outboundconnections','[]'))") && \
	NEW_OB=$$(python3 -c "import sys,json; existing=json.loads('$$EXISTING'); existing.append({\"name\":\"servicebus-loopback\",\"provider\":\"azure-servicebus\",\"message_format\":\"xml\",\"file_transfer_enabled\":False,\"azure_servicebus\":{\"connection_string\":\"$(SERVICEBUS_EMULATOR_CONNSTR)\",\"queue_name\":\"$(SERVICEBUS_QUEUE)\"}}); print(json.dumps(existing))") && \
	EXISTING_IB=$$(curl -sf http://localhost:$(MM_PORT_A)/api/v4/config \
		-H "Authorization: Bearer $$TOKEN_A" | python3 -c "import sys,json; c=json.load(sys.stdin); ps=c.get('PluginSettings',{}).get('Plugins',{}).get('crossguard',{}); print(ps.get('inboundconnections','[]'))") && \
	NEW_IB=$$(python3 -c "import sys,json; existing=json.loads('$$EXISTING_IB'); existing.append({\"name\":\"servicebus-loopback\",\"provider\":\"azure-servicebus\",\"message_format\":\"xml\",\"file_transfer_enabled\":False,\"azure_servicebus\":{\"connection_string\":\"$(SERVICEBUS_EMULATOR_CONNSTR)\",\"queue_name\":\"$(SERVICEBUS_QUEUE)\"}}); print(json.dumps(existing))") && \
	curl -sf -X PUT http://localhost:$(MM_PORT_A)/api/v4/config/patch \
		-H "Authorization: Bearer $$TOKEN_A" \
		-H "Content-Type: application/json" \
		-d '{"PluginSettings":{"Plugins":{"crossguard":{"outboundconnections":"'"$$(echo $$NEW_OB | sed 's/"/\\"/g')"'","inboundconnections":"'"$$(echo $$NEW_IB | sed 's/"/\\"/g')"'"}}}}' >/dev/null && \
	echo "Server A configured with azure-servicebus loopback connection (outbound + inbound)" && \
	echo "Resetting plugin to pick up new config..." && \
	$(DOCKER_COMPOSE) exec -T mattermost-a mmctl --local plugin disable $(PLUGIN_ID) && \
	$(DOCKER_COMPOSE) exec -T mattermost-a mmctl --local plugin enable $(PLUGIN_ID) && \
	sleep 2 && \
	echo "Initializing Service Bus loopback teams..." && \
	curl -sf -X POST http://localhost:$(MM_PORT_A)/api/v4/commands/execute \
		-H "Authorization: Bearer $$TOKEN_A" \
		-H "Content-Type: application/json" \
		-d '{"channel_id":"'"$$SB_LOOP_CH"'","command":"/crossguard init-team outbound:servicebus-loopback"}' >/dev/null && \
	echo "  Server A loop: init-team outbound:servicebus-loopback" && \
	curl -sf -X POST http://localhost:$(MM_PORT_A)/api/v4/commands/execute \
		-H "Authorization: Bearer $$TOKEN_A" \
		-H "Content-Type: application/json" \
		-d '{"channel_id":"'"$$SB_LB_CH"'","command":"/crossguard init-team inbound:servicebus-loopback"}' >/dev/null && \
	echo "  Server A test: init-team inbound:servicebus-loopback" && \
	curl -sf -X POST http://localhost:$(MM_PORT_A)/api/v4/commands/execute \
		-H "Authorization: Bearer $$TOKEN_A" \
		-H "Content-Type: application/json" \
		-d '{"channel_id":"'"$$SB_LOOP_CH"'","command":"/crossguard init-channel outbound:servicebus-loopback"}' >/dev/null && \
	echo "  Server A loop/servicebus-loopback: init-channel outbound:servicebus-loopback" && \
	curl -sf -X POST http://localhost:$(MM_PORT_A)/api/v4/commands/execute \
		-H "Authorization: Bearer $$TOKEN_A" \
		-H "Content-Type: application/json" \
		-d '{"channel_id":"'"$$SB_LB_CH"'","command":"/crossguard init-channel inbound:servicebus-loopback"}' >/dev/null && \
	echo "  Server A test/servicebus-loopback: init-channel inbound:servicebus-loopback" && \
	echo "Setting rewrite-team rule..." && \
	curl -sf -X POST http://localhost:$(MM_PORT_A)/api/v4/commands/execute \
		-H "Authorization: Bearer $$TOKEN_A" \
		-H "Content-Type: application/json" \
		-d '{"channel_id":"'"$$SB_LB_CH"'","command":"/crossguard rewrite-team servicebus-loopback loop"}' >/dev/null && \
	echo "  Server A: rewrite-team servicebus-loopback loop -> test" && \
	echo "Posting Service Bus smoke-test message from Server A loop/servicebus-loopback..." && \
	TOKEN_USERA=$$(curl -sf -X POST http://localhost:$(MM_PORT_A)/api/v4/users/login \
		-d '{"login_id":"usera","password":"password"}' -i 2>/dev/null \
		| grep -i '^Token:' | awk '{print $$2}' | tr -d '\r') && \
	SB_ID=$$(date +%s)-$$$$-sb && \
	curl -sf -X POST http://localhost:$(MM_PORT_A)/api/v4/posts \
		-H "Authorization: Bearer $$TOKEN_USERA" \
		-H "Content-Type: application/json" \
		-d '{"channel_id":"'"$$SB_LOOP_CH"'","message":"servicebus-smoke-test:'"$$SB_ID"'"}' >/dev/null && \
	echo "  Posted servicebus-smoke-test:$$SB_ID to Server A loop/servicebus-loopback" && \
	echo "Waiting for Service Bus relay (3s)..." && \
	sleep 3 && \
	SB_FOUND=$$(curl -sf "http://localhost:$(MM_PORT_A)/api/v4/channels/$$SB_LB_CH/posts?per_page=10" \
		-H "Authorization: Bearer $$TOKEN_A" | python3 -c "import sys,json;data=json.load(sys.stdin);sid='$$SB_ID';found=any('servicebus-smoke-test:'+sid in p.get('message','') for p in data.get('posts',{}).values());print('PASS' if found else 'FAIL');sys.exit(0 if found else 1)") && \
	echo "Service Bus message relay test: $$SB_FOUND" || \
	{ echo "Service Bus message relay FAILED: servicebus-smoke-test:$$SB_ID not found on Server A test/servicebus-loopback"; exit 1; }
