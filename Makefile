.PHONY: build clean deploy deploy-frontend test-ingest seed-demo destroy

STACK_NAME ?= faultline
REGION     ?= $(shell aws configure get region || echo us-east-1)
ACCOUNT_ID ?= $(shell aws sts get-caller-identity --query Account --output text)
ARTIFACT_BUCKET = $(STACK_NAME)-artifacts-$(ACCOUNT_ID)-$(REGION)

# ─── SAM build targets (also works with `sam build`) ────────────
build-IngestFunction:
	GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go build -tags lambda.norpc -ldflags="-s -w" -o $(ARTIFACTS_DIR)/bootstrap ./cmd/ingest

build-QueryFunction:
	GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go build -tags lambda.norpc -ldflags="-s -w" -o $(ARTIFACTS_DIR)/bootstrap ./cmd/query

build-AlerterFunction:
	GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go build -tags lambda.norpc -ldflags="-s -w" -o $(ARTIFACTS_DIR)/bootstrap ./cmd/alerter

# ─── Direct Go build (no Python/SAM CLI dependency required) ────
build:
	mkdir -p .aws-sam/build/IngestFunction .aws-sam/build/QueryFunction .aws-sam/build/AlerterFunction
	GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go build -tags lambda.norpc -ldflags="-s -w" -o .aws-sam/build/IngestFunction/bootstrap ./cmd/ingest
	GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go build -tags lambda.norpc -ldflags="-s -w" -o .aws-sam/build/QueryFunction/bootstrap ./cmd/query
	GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go build -tags lambda.norpc -ldflags="-s -w" -o .aws-sam/build/AlerterFunction/bootstrap ./cmd/alerter

deploy: build
	aws s3 mb s3://$(ARTIFACT_BUCKET) --region $(REGION) 2>/dev/null || true
	aws cloudformation package \
		--template-file template.yaml \
		--s3-bucket $(ARTIFACT_BUCKET) \
		--output-template-file .aws-sam/packaged.yaml \
		--region $(REGION)
	aws cloudformation deploy \
		--template-file .aws-sam/packaged.yaml \
		--stack-name $(STACK_NAME) \
		--capabilities CAPABILITY_IAM CAPABILITY_AUTO_EXPAND \
		--region $(REGION)

deploy-frontend:
	@BUCKET=$$(aws cloudformation describe-stacks \
		--stack-name $(STACK_NAME) \
		--region $(REGION) \
		--query 'Stacks[0].Outputs[?OutputKey==`DashboardBucket`].OutputValue' \
		--output text); \
	echo "Deploying frontend to $$BUCKET ..."; \
	aws s3 sync frontend/ s3://$$BUCKET/ --delete --region $(REGION)

clean:
	rm -rf .aws-sam

destroy:
	@RAW_BUCKET=$$(aws cloudformation describe-stack-resource \
		--stack-name $(STACK_NAME) \
		--logical-resource-id RawPayloadBucket \
		--region $(REGION) \
		--query 'StackResourceDetail.PhysicalResourceId' \
		--output text 2>/dev/null || true); \
	DASH_BUCKET=$$(aws cloudformation describe-stacks \
		--stack-name $(STACK_NAME) \
		--region $(REGION) \
		--query 'Stacks[0].Outputs[?OutputKey==`DashboardBucket`].OutputValue' \
		--output text 2>/dev/null || true); \
	if [ -n "$$RAW_BUCKET" ] && [ "$$RAW_BUCKET" != "None" ]; then aws s3 rm s3://$$RAW_BUCKET --recursive --region $(REGION) || true; fi; \
	if [ -n "$$DASH_BUCKET" ] && [ "$$DASH_BUCKET" != "None" ]; then aws s3 rm s3://$$DASH_BUCKET --recursive --region $(REGION) || true; fi
	aws s3 rb s3://$(ARTIFACT_BUCKET) --force --region $(REGION) 2>/dev/null || true
	aws cloudformation delete-stack --stack-name $(STACK_NAME) --region $(REGION)
	@echo "Stack deletion initiated for $(STACK_NAME) and artifact bucket removed."

# ─── Quick smoke test ───────────────────────────────────────────
test-ingest:
	@API=$$(aws cloudformation describe-stacks \
		--stack-name $(STACK_NAME) \
		--region $(REGION) \
		--query 'Stacks[0].Outputs[?OutputKey==`ApiEndpoint`].OutputValue' \
		--output text); \
	echo "Sending test error to $$API/errors ..."; \
	curl -s -X POST "$$API/errors" \
		-H "Content-Type: application/json" \
		-d '{"message":"null pointer in user handler","source":"api-service","level":"error","stacktrace":"main.go:42 -> handler.go:15","metadata":{"request_id":"abc-123","user_id":"u-456"}}' | python3 -m json.tool

# ─── Populate realistic demo errors & trigger Step Functions ────
seed-demo:
	@API=$$(aws cloudformation describe-stacks \
		--stack-name $(STACK_NAME) \
		--region $(REGION) \
		--query 'Stacks[0].Outputs[?OutputKey==`ApiEndpoint`].OutputValue' \
		--output text); \
	echo "API Endpoint: $$API"; \
	echo "1) Sending 10x payment timeout errors (triggers 1st alert on #1, suppressed alert on #10)..."; \
	for i in $$(seq 1 10); do \
		curl -s -X POST "$$API/errors" -H "Content-Type: application/json" \
			-d "{\"message\":\"stripe webhook signature verification failed\",\"source\":\"payment-service\",\"level\":\"error\",\"stacktrace\":\"webhook.go:88 -> verify.go:31\",\"metadata\":{\"attempt\":\"$$i\",\"region\":\"us-east-1\"}}" > /dev/null; \
	done; \
	echo "2) Sending 3x database pool exhausted warnings..."; \
	for i in 1 2 3; do \
		curl -s -X POST "$$API/errors" -H "Content-Type: application/json" \
			-d "{\"message\":\"connection pool exhausted (max=50)\",\"source\":\"checkout-db\",\"level\":\"warning\",\"stacktrace\":\"pool.go:114 -> query.go:52\",\"metadata\":{\"active_conns\":\"50\",\"wait_ms\":\"1200\"}}" > /dev/null; \
	done; \
	echo "3) Sending 1x auth token error..."; \
	curl -s -X POST "$$API/errors" -H "Content-Type: application/json" \
		-d '{"message":"expired JWT refresh token","source":"auth-gateway","level":"error","stacktrace":"jwt.go:64 -> middleware.go:29","metadata":{"client":"ios-app-v2"}}' > /dev/null; \
	echo "✅ Done! Paste this URL into the dashboard Connect box:"; \
	echo "   $$API"
