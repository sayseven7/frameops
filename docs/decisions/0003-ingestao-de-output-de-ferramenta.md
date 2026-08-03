# ADR 0003: ingestão de output de ferramenta pela API

**Status:** padrão mínimo em vigor; a semântica definitiva de deduplicação
continua sendo decisão pendente da Etapa 0 (item 4.2 do brief mestre).

## Contexto

O PRODUCT.md exige captura no fluxo do pentester por uma CLI Go online-only, com
a API HTTP como único ponto de entrada regular — a única exceção é o bootstrap
local do primeiro admin. O brief mestre pede `fops ingest nmap ./scan.xml` e
lista Nmap, ffuf e Nuclei como ingestores iniciais, mas declara explicitamente
que a semântica de deduplicação é decisão pendente e não deve ser inventada
durante a implementação.

Sem uma decisão registrada, a fatia não pode existir: importar duas vezes o
mesmo scan ou renomear silenciosamente um ativo existente são comportamentos
irreversíveis para um registro que o produto trata como histórico.

## Decisão (padrão mínimo)

1. **Um único ingestor: Nmap XML (`nmaprun`).** ffuf e Nuclei não são
   implementados agora porque produzem itens em forma de finding e dependem da
   decisão pendente sobre severidade importada versus classificação humana. Um
   formato não usado não é adicionado.
2. **A CLI não interpreta o artefato.** `fops ingest nmap ARQUIVO` envia os
   bytes crus para `POST /v1/engagements/{id}/ingestions`; o parsing, os limites
   e a validação acontecem no servidor, na fronteira de confiança, como já
   ocorre com a importação de DOCX. Assim CLI, web e futuros ingestores nunca
   divergem de regra.
3. **Identidade do ativo:** dentro de um engajamento, um ativo é identificado
   pelo nome. O nome derivado de um host Nmap é o primeiro `<hostname>` em
   minúsculas quando existir, senão o endereço. Isso é aplicado por índice único
   `(organization_id, engagement_id, name)` no schema, não por disciplina do
   servidor.
4. **Deduplicação de artefato:** o digest SHA-256 do artefato é único por
   engajamento. Reenviar exatamente o mesmo arquivo responde `409
   duplicate_artifact` com o identificador da ingestão anterior. Duplicidade é
   detectada de forma explícita; nunca importada duas vezes em silêncio.
5. **Ingestão nunca sobrescreve.** Um host já existente é reaproveitado como
   está: a ingestão não renomeia, não altera e não remove ativo algum. Ela só
   pode criar.
6. **Dado importado é separado de interpretação humana:** `assets.source` vale
   `manual` ou `ingest`, e um ativo importado aponta para a ingestão que o
   criou. O schema recusa as duas combinações inconsistentes.
7. **Resumo por ingestão**, gravado de forma imutável junto do registro:
   `read = created + reused + ignored + rejected`. `ignored` são hosts que não
   estão `up` e repetições da mesma identidade dentro do mesmo artefato;
   `rejected` são hosts `up` sem endereço ou hostname utilizável; um artefato
   inválido é recusado inteiro com `400`, sem registro parcial.
8. **Limites em vigor:** 8 MiB por artefato e 4096 hosts. São limites de
   inventário, não de evidência; a matriz definitiva de limites continua aberta.
9. **Proveniência:** cada ingestão registra ferramenta, versão de formato
   detectada (`scanner`, `version`, `xmloutputversion`), nome de arquivo, digest,
   tamanho, ator e horário de recebimento do servidor, com evento de auditoria
   `ingestion.recorded` e um `asset.created` por ativo criado.
10. **Sessão da CLI:** `fops login` guarda o cookie de sessão emitido pela API em
    `$FRAMEOPS_CONFIG_HOME`, `$XDG_CONFIG_HOME/frameops` ou `~/.config/frameops`,
    em arquivo `0600` dentro de diretório `0700`. A CLI recusa uma URL base que
    não seja `https://`, exceto em loopback, porque o cookie de sessão é
    `Secure`. Não existe fila local: a CLI é online-only e uma ingestão que
    falha é repetida.

## Consequências

Continuam pendentes e não foram inventados aqui: preservação do artefato bruto
como evidência (a evidência hoje é ancorada em finding, e a autorização para
guardar o artefato é decisão de produto), ingestão de portas e serviços como
parte do ativo, ingestores em forma de finding com severidade da ferramenta, e
reconciliação entre scans sucessivos (o que "sumiu" de um scan para o outro).

`items_read = items_created + items_reused + items_ignored + items_rejected` é
verificado pelo banco, então um resumo aritmético incoerente não pode ser
gravado. O registro de ingestão é imutável por trigger.

Nenhuma credencial nova foi criada por esta decisão.
