# FrameOPS — Etapa 0: alinhamento e desenho aprovado

**Status:** aguardando revisão final do responsável antes do plano de implementação.

**Fonte canônica:** `FrameOPS-prompt-mestre.md`.

## 1. Resumo do entendimento

FrameOPS é uma plataforma interna, posteriormente open source, para conduzir pentests desde o planejamento até o relatório entregue. O fluxo de maior valor é registrar findings e evidências no momento da descoberta, inclusive pelo terminal, sem manter notas paralelas.

O produto atende usuários membros de organizações. A API Go é a porta de entrada normal para CLI, web e ingestores; PostgreSQL armazena dados estruturados, autorização, auditoria e estados; MinIO/S3 compatível armazena os bytes de evidências e documentos. O backend, a CLI e o domínio são em Go; a web é Next.js/React; o frontend usará pnpm, Tailwind, `react-icons`, i18n, tokens e temas claro/escuro/sistema.

Os invariantes prioritários são confidencialidade, integridade, cadeia de custódia, isolamento entre organizações, regras de domínio determinísticas e proveniência DOCX aprovado → PDF. O domínio não depende de HTTP: a API usa `net/http` e `chi`, mas traduz suas requisições para contratos de aplicação e regras de domínio testáveis sem servidor.

## 2. Decisões aprovadas

| Decisão | Resultado | Impacto |
|---|---|---|
| Fronteira de ownership | Organização/equipe é o owner; usuários são membros. | Schema e API nas Etapas 2 e 4. |
| Papéis MVP | `admin` e `member`; sem superadministrador entre organizações. | Autorização e auditoria na Etapa 4. |
| Isolamento | Acesso a recurso de outra organização retorna 404 para usuários comuns. | Queries e testes de autorização. |
| Evidência e retenção | Imutabilidade lógica, sem expiração automática. Descarte físico é excepcional, autorizado/auditado, preserva tombstone e respeita legal hold. | Etapa 6 e migrations correspondentes. |
| Banco e storage | Upload passa por estados intermediários e reconciliador; não há transação distribuída alegada. | Etapa 6. |
| Offline | CLI é online-only no MVP; não existe fila offline. | Etapa 9. |
| Plataformas da CLI | Linux, macOS e Windows; Linux cobre X11 e Wayland. | Adaptadores e matriz de testes na Etapa 9. |
| Renderer | Worker/container isolado, sem acesso direto a banco, storage ou internet; recebe apenas arquivos temporários e dados mínimos. | Etapa 10. |
| Relatórios | Revisões DOCX são imutáveis; somente uma revisão fica aprovada por relatório; PDF deriva exclusivamente dela e registra seu hash. | Etapa 10. |
| Credenciais de teste | Fora do MVP; nenhum cofre genérico será criado. | Evita escopo nas Etapas 1–11. |
| CVSS | MVP suporta CVSS v3.1, validado contra vetores FIRST; fronteira versionada prepara v4.0 futuro. | Etapa 3. |
| Bootstrap | Primeiro admin via CLI local com credencial externa ao banco, destruída após uso; não há endpoint HTTP de bootstrap. | Etapa 4. |
| Idiomas | `pt-BR` é o idioma inicial; `en` também integra o MVP. | Etapa 11. |
| Metodologias | Templates originais, estruturados, atribuídos e versionados; não copiar texto extenso das fontes. | Etapa 7. |
| Biblioteca de templates | Templates são personalizáveis, podem ser criados e importados em pacote JSON versionado e validado. Colisão de ID cria nova versão, nunca sobrescreve. | Etapa 7. |
| Permissões de templates | `member` cria/edita rascunhos próprios; `admin` importa e publica na biblioteca da organização. | Etapas 4 e 7. |

## 3. Arquitetura aprovada

```text
CLI / Web / Ingestores
          │ HTTP (net/http + chi)
          ▼
    aplicação Go ──► domínio Go puro
       │       │
       │       ├── PostgreSQL: dados, ownership, auditoria, estados
       │       └── MinIO/S3: blobs imutáveis
       │
       └── contrato de job ──► worker isolado: docxtpl / LibreOffice
```

- O domínio não importa HTTP, SQL, S3, CLI ou renderer. A aplicação coordena contratos e transações.
- Toda mutação estruturada persiste o evento de auditoria na mesma transação PostgreSQL.
- Upload cria um registro `pending`, detecta o tipo por conteúdo, calcula SHA-256 sobre os bytes persistidos e envia em streaming ao storage. Depois, a aplicação marca a evidência como disponível. Falhas ficam visíveis e reconciliáveis.
- Templates e revisões de relatório são versões imutáveis. Um engajamento recebe uma cópia publicada de template; alterar a biblioteca nunca muda a instância existente.
- O worker de documento tem filesystem temporário, limites de recursos e sem egress. A aplicação persiste e audita resultados; falha não cria artefato aprovado.

## 4. Contradições e tensões

| Item | Classificação | Resolução ou encaminhamento |
|---|---|---|
| Evidência “indeletável” versus obrigação legal | Bloqueador resolvido | Imutabilidade lógica, sem expiração automática e descarte excepcional com tombstone, autorização, auditoria e legal hold. |
| Auditoria transacional versus blobs fora do PostgreSQL | Bloqueador resolvido | Estados intermediários, compensação/reconciliação e sem promessa de atomicidade distribuída. |
| Multiusuário versus ownership | Bloqueador resolvido | Organização é owner, com `admin` e `member`; schema e API reforçam isolamento. |
| Go versus `docxtpl` Python | Bloqueador resolvido | Worker isolado por contrato explícito. |
| Reimportação manual versus proveniência | Bloqueador resolvido | Revisões imutáveis e PDF somente do DOCX explicitamente aprovado. |
| Finding removível versus evidência permanente | Importante | A Etapa 2/6 deve proibir cascata destrutiva e usar tombstones para exclusões lógicas. |
| Credenciais cifradas sem domínio definido | Bloqueador resolvido | Fora do MVP. |
| Captura em cinco segundos versus rede indisponível | Bloqueador resolvido | MVP online-only, com falha explícita e sem fila local. |
| Sete metodologias versus licença/manutenção | Importante | Conteúdo original estruturado, atribuído e versionado; revisão técnica e fontes precisam constar por pacote. |
| CVSS 3.1 versus evolução do padrão | Bloqueador resolvido | v3.1 agora, vetores FIRST e fronteira versionada para versão futura. |
| 404 entre owners versus administração | Bloqueador resolvido | Sem administrador cross-org no MVP; administração fica dentro da organização e é auditada. |
| Open source posterior versus dados reais | Importante | Defaults seguros, fixtures sintéticas, limpeza de histórico e disclosure são gates da Etapa 12. |
| Template editável versus histórico | Bloqueador resolvido | Drafts e biblioteca são versionados; instâncias do engajamento são cópias imutáveis. |

## 5. Threat sketch inicial

| Categoria | Conteúdo |
|---|---|
| Ativos | Evidências, findings, escopo/ROE, relatórios, hashes, sessões, PATs, auditoria e credenciais de infraestrutura. |
| Atores | Pentester membro, administrador da organização, atacante externo, membro malicioso, processo de ingestão, worker de documentos e operador de infraestrutura. |
| Fronteiras de confiança | Clientes HTTP, parser de uploads/importações, PostgreSQL, object storage, worker de renderer, browser, estação local e CI. |
| Entradas não confiáveis | Credenciais de login, PATs, parâmetros HTTP, arquivos de evidência, outputs de ferramentas, JSON de templates, DOCX reimportado e imagens. |
| Ameaças principais | Enumeração de contas, acesso cross-org, sequestro de sessão, upload malicioso/truncado, parser bomb, SSRF, objecto órfão, adulteração de evidência, PDF de fonte incorreta, secret em logs e execução maliciosa no renderer. |
| Controles propostos | Autorização server-side e chaves de ownership; 404 cross-org; sessão revogável e PATs com escopo; custo criptográfico semelhante no login; streaming com limites e tipo detectado por conteúdo; SHA-256 server-side; auditoria append-only; estados e reconciliação; worker sem rede/privilégios; dados sintéticos em testes e logs redigidos. |

O threat model completo é exigido antes da publicação open source e deve evoluir junto de cada superfície implementada.

## 6. Critérios observáveis por etapa

| Etapa | Critério de aceite |
|---|---|
| 1 — Estrutura | Toolchains e Docker Compose reproduzíveis; lint, teste mínimo e documentação de desenvolvimento passam sem dados reais. |
| 2 — Infra e schema | Migrations aplicam do zero e sobre a versão anterior; constraints, ownership e auditoria de schema têm testes reais. |
| 3 — Domínio | CVSS v3.1 e máquinas de estado possuem testes determinísticos, incluindo transições negadas. |
| 4 — API e autenticação | Sessões revogáveis, PATs, bootstrap local, 404 cross-org e auditoria de mutações são demonstrados por testes. |
| 5 — Findings | Templates de finding são copiados para findings e vínculos com ativos preservam histórico. |
| 6 — Evidências | Streaming, hash server-side, detecção de conteúdo, append-only, acesso autorizado e reconciliação são testados com storage real. |
| 7 — Metodologias | Sete metodologias atribuídas existem; drafts, publicação, JSON importado validado e cópias imutáveis são testados. |
| 8 — Retest | Rodadas imutáveis produzem estado atual derivado e preservam evidências e justificativas. |
| 9 — CLI | A CLI usa somente a API; contexto ativo, captura de evidência e ingestores produzem resumos e suportam Linux X11/Wayland, macOS e Windows. |
| 10 — Relatórios | DOCX versionado e reimportado, aprovação explícita, PDF isolado e vínculo PDF → hash exato do DOCX aprovado são demonstrados. |
| 11 — Web UI | Next.js/React com `pt-BR` e `en`, tema claro/escuro/sistema, tokens Tailwind, acessibilidade e os fluxos operacionais previstos funciona contra a API. |
| 12 — Open source | Threat model final, CI, licença/atribuições, disclosure, documentação e auditoria de histórico concluídos. |

Toda etapa com código também exige formatação, lint, testes unitários relevantes, integração com dependências reais quando aplicável, build reproduzível, documentação, testes negativos nas superfícies de segurança e nenhuma credencial/evidência real em artefatos de desenvolvimento.

## 7. Pendências deliberadas e gates

As decisões abaixo não devem ser inventadas. Elas não bloqueiam a consolidação da Etapa 0, mas bloqueiam a etapa indicada antes que seu código seja iniciado.

| Pendente | Gate |
|---|---|
| Versões suportadas de Go, PostgreSQL, MinIO, Next.js, React, Tailwind, pnpm, Python/docxtpl e LibreOffice | Antes da Etapa 1. |
| Limites de arquivo, lote e engajamento; timeouts e concorrência | Antes da Etapa 6. |
| Matriz concreta de transições de finding e política de justificativas | Antes da Etapa 3. |
| Escopos de PAT, expiração/revogação e política de sessão/CSRF | Antes da Etapa 4. |
| Semântica de deduplicação e corpus de Nmap, ffuf e Nuclei | Antes da Etapa 9. |
| Estratégia de screenshot por ambiente e mecanismo de preservação local após falha | Antes da Etapa 9. |
| Fontes, versões, atribuições e revisão técnica dos sete templates | Antes da Etapa 7. |
| SLA: origem, pausas, timezone, exceções e efeito de risco aceito | Antes da Etapa 8, se SLA entrar. |
| Navegadores suportados, metas de acessibilidade e paleta visual final | Antes da Etapa 11. |
| RPO/RTO, backup/restore, métricas, alertas e disponibilidade | Antes da Etapa 6 para dados reais; detalhar na Etapa 12. |

## 8. Plano revisado

A sequência original de 12 etapas é aprovada. Não há reordenação por preferência. As seguintes dependências ficam explícitas:

1. A Etapa 1 só começa após fixar a matriz de versões suportadas.
2. A Etapa 2 estabelece constraints e migrations antes de qualquer CRUD.
3. A Etapa 3 antecede a API para que HTTP, CLI e web não definam regras divergentes.
4. A Etapa 6 antecede a CLI, pois captura sem um mecanismo seguro de evidência não entrega valor confiável.
5. A Etapa 7 inclui criação, importação e publicação versionada de metodologias; não deve ser reduzida a dados estáticos.
6. A Etapa 10 depende de dados de findings, evidências, checklist e retests já versionados.
7. A Etapa 11 implementa a UI sobre contratos de API estáveis, usando o contexto registrado em `PRODUCT.md` e uma direção visual aprovada antes de componentes.

## 9. Gate para a próxima fase

Antes de criar código, scaffold ou migration, esta especificação precisa de aprovação final. Em seguida, a próxima decisão é a matriz de versões da Etapa 1; com ela aprovada, será escrito um plano de implementação detalhado e revisável.
