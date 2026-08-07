# ADR 0004: contrato operacional do runtime Compose

**Status:** em vigor para o runtime local atual; não constitui aprovação de
release nem runbook de produção.

## Contexto

O Compose reúne PostgreSQL, MinIO, migração, renderer, API e web. A topologia
tem contratos verificáveis para dependências, healthchecks e isolamento, mas o
repositório não contém automação de backup, restore ou rollback coordenado.
Documentar capacidades ausentes como se existissem criaria uma recuperação
perigosa para dados estruturados e bytes de evidência.

## Decisão

### Versões e identidades aceitas

| Item | Contrato atual | Fonte verificável |
| --- | --- | --- |
| Go | >= 1.26.5 | `go.mod`, `scripts/check-toolchains.sh` |
| Node.js | >= 22.12.0 | `scripts/check-toolchains.sh`, `Dockerfile.web` |
| pnpm | major 10 (lockfile congelado) | `package.json`, `Makefile` |
| Python | >= 3.13.0 | `scripts/check-toolchains.sh` |
| Docker Compose | >= 2.20.0 | `scripts/check-toolchains.sh` |
| golangci-lint | v2.9.0 compilado com Go 1.26.5 | `scripts/check-toolchains.sh` |
| PostgreSQL | `postgres:18.4@sha256:3a82e1f56c8f0f5616a11103ac3d47e632c3938698946a7ad26da0df1334744a` | `compose.yaml` |
| MinIO | `minio/minio:RELEASE.2025-09-07T16-13-09Z@sha256:14cea493d9a34af32f524e538b8346cf79f3321eff8e708c1e2960462bd8936e` | `compose.yaml` |

API, migrator, renderer e web são construídos do checkout pelo Compose; não há
imagem publicada ou identidade de release separada para restaurar um build
anterior.

### Recursos, rede e estado

| Serviço | Limite | Reserva | Estado/rede |
| --- | --- | --- | --- |
| postgres | 1 CPU, 1 GiB | 0,25 CPU, 512 MiB | volume `frameops-postgres-data`, porta loopback |
| minio | 1 CPU, 1 GiB | 0,25 CPU, 512 MiB | volume `frameops-minio-data`, porta loopback, sem restart automático |
| renderer | 1 CPU, 1 GiB, 128 PIDs, `/tmp` 256 MiB | nenhuma | rede `none`, root FS somente leitura, sem portas, socket compartilhado |
| migrate, api, web | nenhum limite/reserva declarado | nenhum | não se deve inferir capacidade a partir deste Compose |

O renderer recebe somente `FRAMEOPS_RENDER_SOCKET`; API e renderer compartilham
apenas `frameops-render-socket`. Portanto o worker não recebe banco, S3 ou rede
por esta fronteira. PostgreSQL, MinIO, API e web só publicam portas em
`127.0.0.1`.

### Deploy e prontidão

O único fluxo declarado é `docker compose --env-file .env up --build --wait`,
com os dois FIFOs de MinIO presentes. `migrate` executa somente `up`; API espera
PostgreSQL e MinIO saudáveis, migrator concluído e renderer saudável; web espera
API saudável. Os checks são, respectivamente, `pg_isready`,
`/minio/health/live`, healthcheck do socket do renderer, `GET /health` da API e
`GET /login` da web. `docker compose --env-file .env ps` é a inspeção
operacional sem efeito colateral.

### Parada, backup, restore e upgrade

`docker compose --env-file .env down --timeout 10` preserva os volumes nomeados;
`down -v`, `--remove-orphans` e exclusão de volumes não fazem parte do contrato.
O launcher `scripts/local-runtime.sh` passa o project-name exato derivado do
diretório de estado e também preserva seus volumes nomeados. Ele não seleciona
volumes externos ao projeto.

Não existe comando, formato ou teste de backup/restore que cubra juntos
PostgreSQL e MinIO. Consequentemente, restore após perda de dados e rollback
após migração bem-sucedida são **não suportados** pelo repositório. Antes de um
upgrade, o operador deve ter backups externos consistentes e validados dos dois
stores. Se a falha ocorrer antes da migração concluir, pare sem remover volumes,
corrija a configuração e repita a subida. `frameops-migrate down-to VERSION`
existe, mas não é um rollback operacional aprovado: migrations podem recusar ou
destruir estado, e ele não restaura bytes do MinIO.

## Consequências

Os scripts de contrato validam a estrutura do Compose, incluindo imagens,
recursos, isolamento do renderer, portas, dependências e healthchecks. Eles não
provam lifecycle real neste ambiente quando Docker Compose V2 não está
disponível. Uma entrega que necessite recuperação de dados deve adicionar um
contrato explícito e testado de backup/restore antes de poder alegá-la.
