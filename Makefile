.PHONY: build test fmt vet staticcheck lint config-validate simulate reliability terraform-fmt terraform-validate eks-kubeconfig eks-nodes aws-platform-render aws-demo-render aws-apply-platform aws-apply-demo aws-wait-platform aws-wait-demo aws-smoke aws-delete-k8s terraform-destroy dev-up dev-down migrate-up clean

COMPOSE_FILE := deploy/docker-compose/docker-compose.yml
AWS_TERRAFORM_DIR := deploy/terraform/aws

build:
	go build ./cmd/...

test:
	go test ./...

fmt:
	go fmt ./...

vet:
	go vet ./...

staticcheck:
	go run honnef.co/go/tools/cmd/staticcheck@v0.8.1 ./...

lint: vet staticcheck

config-validate:
	go run ./cmd/cfgctl validate examples/config-as-code

simulate:
	go run ./cmd/simulator -agents 1000 -concurrency 64

reliability:
	go run ./cmd/reliability -scenario all -concurrency 32

terraform-fmt:
	terraform -chdir=$(AWS_TERRAFORM_DIR) fmt

terraform-validate:
	terraform -chdir=$(AWS_TERRAFORM_DIR) validate

eks-kubeconfig:
	aws eks update-kubeconfig --region $$(terraform -chdir=$(AWS_TERRAFORM_DIR) output -raw aws_region) --name $$(terraform -chdir=$(AWS_TERRAFORM_DIR) output -raw eks_cluster_name)

eks-nodes:
	kubectl get nodes

aws-platform-render:
	kubectl kustomize deploy/kubernetes/aws/platform

aws-demo-render:
	kubectl kustomize deploy/kubernetes/aws/demo

aws-apply-platform:
	kubectl apply -k deploy/kubernetes/aws/platform

aws-apply-demo:
	kubectl apply -k deploy/kubernetes/aws/demo

aws-wait-platform:
	kubectl -n safe-config-system wait --for=condition=available deploy/postgres --timeout=180s
	kubectl -n safe-config-system wait --for=condition=complete job/safeconfig-migrations --timeout=180s
	kubectl -n safe-config-system wait --for=condition=available deploy/control-plane --timeout=180s
	kubectl -n safe-config-system wait --for=condition=available deploy/data-plane --timeout=180s
	kubectl -n safe-config-system wait --for=condition=available deploy/prometheus --timeout=180s

aws-wait-demo:
	kubectl -n demo rollout status deploy/payment-demo-service --timeout=240s

aws-smoke:
	kubectl -n safe-config-system get deploy,svc,pods
	kubectl -n demo get deploy,svc,pods

aws-delete-k8s:
	kubectl delete -k deploy/kubernetes/aws/demo --ignore-not-found
	kubectl delete -k deploy/kubernetes/aws/platform --ignore-not-found

terraform-destroy:
	terraform -chdir=$(AWS_TERRAFORM_DIR) destroy

dev-up:
	docker compose -f $(COMPOSE_FILE) up -d

dev-down:
	docker compose -f $(COMPOSE_FILE) down --remove-orphans

migrate-up:
	docker compose -f $(COMPOSE_FILE) exec -T postgres sh -c 'for f in /migrations/*.sql; do psql -U safe_config -d safe_config -f "$$f"; done'

clean:
	go clean
