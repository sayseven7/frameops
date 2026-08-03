# FrameOPS — Etapa 2: infraestrutura e schema aprovado

**Status:** aprovado para implementação em 03/08/2026.

## Objetivo

Estabelecer infraestrutura local reproduzível de PostgreSQL e MinIO e o schema relacional inicial que reforça ownership por organização e auditoria append-only. Esta etapa não entrega comportamento de produto.

## Decisões aprovadas

| Decisão | Resultado aprovado |
|---|---|
| Identificador de usuário | Cada usuário recebe UUID gerado no servidor e e-mail único sem distinção entre maiúsculas/minúsculas. Não há senha, segredo ou credencial de autenticação nesta etapa. |
| Hierarquia inicial | `organização → cliente → engajamento → ativo`, com UUID por entidade e chaves estrangeiras compostas que incluem `organization_id`, impedindo vínculo entre organizações diferentes. |
| Associação e papéis | Usuários entram na organização por membership única `(organization_id, user_id)`; os únicos papéis são `admin` e `member`. |
| Auditoria | `audit_events` é append-only e armazena organização, ator opcional, ação, alvo, resultado, correlação, contexto mínimo em JSON e horário do servidor. Não registra segredo, token, senha ou conteúdo de evidência. |
| Exclusão | Não existe `ON DELETE CASCADE`. Exclusões de pais referenciados são rejeitadas. Exclusão lógica, tombstones, legal hold e descarte físico pertencem às etapas que definirem o ciclo de vida correspondente. |
| MinIO | O Compose local usa `minio/minio:RELEASE.2025-09-07T16-13-09Z@sha256:a1a8bd4ac40ad7881a245bab97323e18f971e4d4cba2c2007ec1bedd21cbaba2`, a imagem `linux/amd64` verificada no registry em 03/08/2026. Possui volume nomeado, porta S3 limitada a localhost e health check; não expõe o console nem cria bucket, objeto, URL assinada ou adaptador de storage. |

## Limites explícitos

Ficam fora da Etapa 2:

- autenticação, bootstrap, sessões, PATs, hashes de senha, endpoints, autorização de requisições e CRUD;
- CVSS e máquinas de estado;
- findings, evidências, uploads, buckets, reconciliação e object storage funcional;
- metodologias, retests, relatórios, renderer, CLI funcional e UI de produto;
- dados reais, segredos, credenciais ou evidências em testes, documentação e Git.

## Regras de migration

1. Migrations aplicadas são imutáveis; correções entram em uma nova migration.
2. O schema deve aplicar do zero e em banco descartável real no PostgreSQL do Compose.
3. Testes precisam demonstrar as constraints de ownership, os papéis permitidos, a ausência de cascade delete e a imutabilidade de auditoria.
4. O domínio futuro permanece independente de HTTP, SQL, S3, CLI e renderer.

## Critérios de aceite

- PostgreSQL e MinIO locais usam referências de imagem com tag e `sha256` exatos.
- Os serviços ficam saudáveis no Compose e a porta S3 é somente localhost.
- O lifecycle de migration é reprodutível em banco descartável, sem tocar o banco de desenvolvimento persistente.
- A organização é a fronteira dura de ownership demonstrada por constraints e testes reais.
- `audit_events` não aceita `UPDATE` nem `DELETE` no banco.
- O worktree não contém segredos, dados de cliente, evidências reais, comportamento HTTP, autenticação, CRUD ou storage funcional.
