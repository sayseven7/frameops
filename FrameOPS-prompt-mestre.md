# FrameOPS — prompt mestre de produto, arquitetura e execução

## Como usar este documento

Este é o contexto canônico do FrameOPS para agentes de engenharia e revisores humanos. Ele combina visão de produto, requisitos, invariantes, arquitetura, plano de implementação e protocolo de trabalho.

Ao receber este documento, não comece implementando. Execute primeiro a **Etapa 0 — alinhamento e revisão crítica**. Aponte contradições, lacunas, riscos e decisões irreversíveis; proponha opções com trade-offs; aguarde aprovação explícita antes de alterar escopo ou escrever código.

---

## 0. Papel e missão do agente

Atue como arquiteto de software, engenheiro Go sênior e revisor de segurança. Sua missão é projetar e implementar o FrameOPS de forma incremental, verificável e adequada ao tratamento de dados altamente sensíveis de pentest.

Prioridades, nesta ordem:

1. integridade e confidencialidade dos dados;
2. captura de evidência sem atrito durante o teste;
3. rastreabilidade e cadeia de custódia;
4. correção das regras de domínio;
5. geração reproduzível do entregável;
6. simplicidade operacional e manutenibilidade;
7. interface visual.

### Regras de execução

- Não invente requisito, regra de negócio, limite ou decisão arquitetural ausente.
- Diferencie claramente: **decidido**, **proposto**, **pendente** e **fora do escopo**.
- Antes de adicionar escopo, apresente a necessidade, as opções, os trade-offs e peça autorização.
- Não silencie contradições para conseguir avançar.
- Não implemente uma etapa sem que seus critérios de entrada estejam satisfeitos.
- Entregue mudanças pequenas e revisáveis; cada etapa deve terminar em artefato executável e evidência de verificação.
- Não altere migrations já aplicadas; toda correção de schema é uma nova migration.
- Não crie atalhos que permitam à CLI, à web ou aos ingestores acessar o banco diretamente.
- Segurança não deve depender apenas de disciplina da aplicação quando puder ser reforçada pelo schema, por isolamento de processo/rede ou por testes automatizados.
- Quando houver mais de uma solução válida, recomende uma, explique por quê e aguarde decisão se ela afetar produto, segurança, compatibilidade ou operação.

### Formato obrigatório ao concluir cada etapa

1. **Resumo:** o que foi entregue.
2. **Decisões aplicadas:** decisões já aprovadas usadas na implementação.
3. **Arquivos e migrations:** o que foi criado ou alterado.
4. **Verificação executada:** comandos e resultados reais de testes, lint, build e checks de segurança relevantes.
5. **Critérios de aceite:** lista objetiva, com cada item marcado como aprovado ou pendente.
6. **Riscos e pendências:** sem esconder débito ou incerteza.
7. **Próxima etapa:** critérios de entrada e qualquer aprovação necessária.

---

## 1. Visão do produto

O FrameOPS é uma plataforma de gestão de projetos de pentest, evidências, findings, retests e relatórios. O backend e a API serão escritos em Go; o frontend será uma aplicação Next.js sobre React. O produto será usado internamente primeiro e publicado como open source depois.

A promessa central é simples:

> O dado entra uma vez, no momento em que é descoberto, e sai como entregável sem retrabalho.

### Entrada, armazenamento e saída

- **Entrada:** CLI durante o teste; ingestão de outputs de ferramentas; web UI para planejamento, revisão e consolidação.
- **Armazenamento:** PostgreSQL para dados estruturados; MinIO/S3 para blobs de evidência; integridade verificável.
- **Saída:** DOCX editável gerado a partir dos dados e PDF convertido exclusivamente da revisão aprovada desse DOCX.

### Critério máximo de sucesso

Usar o FrameOPS em um pentest real, do planejamento ao relatório entregue, sem precisar abrir o Obsidian ou manter um arquivo paralelo para registrar findings e evidências.

Se em qualquer momento for mais rápido colar o material num arquivo de texto, existe um problema de atrito. Atrito não é detalhe de UX: é a falha central que o FrameOPS existe para eliminar.

---

## 2. Problemas que o produto resolve

| # | Dor | Resultado esperado |
|---|-----|--------------------|
| 1 | **Sem visão de portfólio.** Vários projetos simultâneos, em estágios diferentes, sem visão consolidada. | Dashboard com estado, prazo, cobertura, findings e retests pendentes por engajamento. |
| 2 | **Evidência se perde durante o teste.** Screenshots, requests e outputs ficam espalhados. | Captura e vinculação imediatas a partir do terminal ou de ingestores. |
| 3 | **Relatório consome tempo demais.** Descrições, estrutura e formatação são refeitas manualmente. | Templates reutilizáveis e geração de DOCX com edição final controlada. |
| 4 | **Sem controle de retest.** Não há histórico confiável do que foi corrigido em cada rodada. | Rodadas sequenciais, resultados imutáveis/rastreáveis e visão do estado atual. |

A dor 2 tem prioridade operacional. Ferramentas semelhantes fracassam quando viram apenas um CRUD: o pentester continua usando notas paralelas porque preencher um formulário interrompe a exploração.

**Captura sem atrito não é uma feature; é requisito de adoção.**

---

## 3. Escopo e não objetivos

### O FrameOPS é

- gestor de clientes, engajamentos, ativos, escopo e regras de engajamento;
- executor e registrador de checklist de metodologia;
- biblioteca e instanciador de templates de finding;
- repositório de evidências com integridade verificável;
- controlador de retests;
- gerador e versionador de relatórios;
- plataforma local/multiusuário acessada por API.

### O FrameOPS não é

- scanner de vulnerabilidades;
- editor de texto completo com template;
- SaaS multi-tenant com billing;
- ferramenta de colaboração em tempo real;
- substituto de Burp Suite, Nmap, Nuclei, ffuf ou ferramentas equivalentes.

Integração e ingestão não transformam o FrameOPS no produto integrado. Tentar assumir dois papéis enfraquece o fluxo principal.

---

## 4. Fluxos críticos e metas mensuráveis

### 4.1 Captura no terminal

A captura acontece onde o pentester já está. Exemplos de intenção de interface:

```text
fops finding add "SQLi no login"
fops ev add ./shot.png --caption "payload refletido"
fops ingest nmap ./scan.xml
fops shot
```

`fops shot` deve capturar a tela e anexar o arquivo ao contexto ativo em um único fluxo.

Meta principal: **anexar uma evidência em menos de cinco segundos, sem sair do terminal**, desconsiderando apenas o tempo de transferência de arquivos excepcionalmente grandes.

Antes de implementar a CLI, a Etapa 0 deve definir:

- como o contexto ativo é selecionado: cliente, engajamento, finding e/ou ativo;
- comportamento sem contexto ou com contexto ambíguo;
- sistemas operacionais suportados pelo comando de screenshot;
- limites e comportamento em rede indisponível;
- se haverá fila local/offline e, se houver, sua criptografia, expiração, reenvio e deduplicação.

### 4.2 Ingestão

Ingestores iniciais: Nmap, ffuf e Nuclei. Cada ingestão deve:

- preservar o artefato bruto original como evidência quando autorizado;
- registrar ferramenta, formato/versão detectada, horário e hash;
- validar conteúdo e limites antes de processar;
- ser idempotente ou detectar duplicidade de maneira explícita;
- preservar a severidade original da ferramenta;
- separar dado importado de interpretação humana;
- produzir resumo de itens criados, atualizados, ignorados e rejeitados.

A semântica de deduplicação é **decisão pendente da Etapa 0**; não deve ser inventada durante a implementação.

### 4.3 Relatório

Fluxo obrigatório:

```text
dados estruturados
    → DOCX gerado
        → edição humana no Word/LibreOffice
            → reimportação como nova revisão imutável
                → aprovação explícita de uma revisão
                    → PDF convertido exatamente da revisão aprovada
```

Requisitos:

- existe um único pipeline de conteúdo; o PDF nunca é renderizado diretamente dos dados;
- cada geração, reimportação, aprovação e conversão registra ator, horário, versão e hash;
- reimportar não sobrescreve uma revisão anterior;
- aprovar identifica exatamente o blob DOCX aprovado;
- o PDF registra o hash do DOCX de origem e a versão do conversor;
- alterar dados do projeto após a aprovação não modifica silenciosamente o relatório aprovado;
- falha de conversão não pode produzir artefato marcado como aprovado.

A edição manual faz parte do produto. O objetivo não é eliminar a revisão humana, mas garantir que o PDF seja derivado do mesmo DOCX revisado e aprovado.

---

## 5. Conceitos centrais de domínio

### 5.1 Template de metodologia — o que testar

Um template é um checklist executável por tipo de engajamento. Ao criar um engajamento, o sistema copia uma versão do template e instancia seus itens. O pentester registra o que executou, o resultado e, quando aplicável, a justificativa para não executar.

Isso alimenta:

- controle de cobertura durante o teste;
- rastreabilidade de execução;
- seção de metodologia do relatório derivada do trabalho real.

Templates iniciais:

1. pentest web grey box;
2. web black box;
3. infraestrutura interna;
4. black box externo;
5. mobile;
6. API REST;
7. wireless.

Referências: OWASP WSTG, OWASP MASTG, OWASP API Security Top 10, PTES e NIST SP 800-115. Todo conteúdo deve respeitar licenças, atribuição e versionamento das fontes.

Cada item precisa explicar **como verificar**, não apenas nomear o controle. “Testar XSS” é insuficiente. Um item deve conter, conforme aplicável: objetivo, pré-condições, procedimento, evidência esperada, resultado possível, referência e observações de segurança.

Editar um template não altera checklists já instanciados.

### 5.2 Template de finding — como escrever

Biblioteca reutilizável com:

- título e descrição genérica;
- impacto;
- remediação;
- referências;
- CWE e categoria OWASP;
- vetor CVSS sugerido e justificativa do valor padrão.

Ao ser usado, o template é **copiado** para um finding. Não existe referência viva capaz de alterar um finding ou relatório histórico após uma edição do template.

O finding instanciado contém ainda os dados específicos do alvo, reprodução, evidências, ativos afetados, classificação contextual, estados, histórico e retests.

### 5.3 Finding e seus dois eixos de estado

Um finding responde a perguntas independentes.

**Validação — isto é real?**

- novo;
- precisa de revisão;
- confirmado;
- falso positivo.

**Remediação — foi resolvido?**

- não aplicável enquanto o finding não estiver confirmado;
- aberto;
- corrigido;
- risco aceito;
- não reproduzido.

A máquina de estados deve definir transições permitidas, ator, justificativas obrigatórias e efeitos derivados. Falso positivo não recebe estado de remediação.

A diferença entre **corrigido** e **não reproduzido** deve permanecer explícita: ausência de reprodução não prova necessariamente a implementação da correção.

### 5.4 Severidade original e severidade contextual

A classificação importada de uma ferramenta nunca é sobrescrita. O sistema mantém separadamente:

- severidade e dados originais da ferramenta;
- severidade contextual definida pelo pentester;
- vetor e score CVSS calculados;
- justificativa obrigatória para divergência, descarte ou rebaixamento.

Antes de implementar o motor, a Etapa 0 deve decidir o escopo de CVSS 3.1, compatibilidade futura com CVSS 4.0 e a política de arredondamento com vetores oficiais do FIRST como testes de conformidade.

### 5.5 Retest

Retests são rodadas sequenciais vinculadas ao finding e ao engajamento. Cada resultado deve registrar:

- número da rodada;
- data, executor e ambiente;
- procedimento executado;
- resultado observado;
- evidências novas;
- estado anterior e estado resultante;
- justificativa;
- vínculo com a correção declarada, quando houver.

Uma nova rodada não sobrescreve a anterior. O “estado atual” é derivado do histórico válido, não mantido como narrativa manual divergente.

SLA por severidade só deve ser implementado após definir origem do prazo, pausas, timezone, exceções e efeito de risco aceito.

---

## 6. Cadeia de custódia da evidência

Evidência é prova, não mero anexo.

### Invariantes

- SHA-256 calculado no servidor sobre os bytes efetivamente persistidos;
- timestamp de captura informado pelo cliente, quando disponível, e timestamp de recebimento do servidor, sem confundir os dois;
- tipo detectado pelo conteúdo, nunca confiado apenas à extensão ou ao `Content-Type` enviado;
- upload em streaming com limites explícitos;
- append-only: correção gera nova evidência com vínculo à anterior;
- nenhuma atualização substitui bytes, hash ou metadados históricos;
- acesso é autorizado no servidor e registrado em auditoria;
- URLs assinadas têm duração curta e não substituem autorização;
- download permite verificar hash e tamanho;
- a leitura dos bytes usados para detectar o tipo não pode truncar o stream persistido.

### Persistência entre banco e object storage

PostgreSQL e S3/MinIO não compartilham uma transação ACID. O projeto deve definir e testar uma ordem de escrita, estados intermediários, compensação, reconciliação e tratamento de órfãos. Não declarar atomicidade inexistente.

### Exclusão e retenção — decisão crítica pendente

O requisito original diz que evidências são indeletáveis por qualquer caminho, mas o sistema também precisa tratar retenção contratual, dados pessoais, erro de escopo, comprometimento de chave e obrigações legais.

A Etapa 0 deve apresentar uma política coerente entre:

- imutabilidade lógica e cadeia de custódia;
- retenção configurável;
- legal hold;
- descarte autorizado e auditável;
- eventual destruição criptográfica;
- permanência de hashes e metadados após descarte do blob.

Até essa decisão ser aprovada, não implementar exclusão física nem prometer “indeletável” como propriedade absoluta.

---

## 7. Modelo de dados conceitual

```text
Usuário / fronteira de ownership
└── Cliente
    └── Engajamento
        ├── escopo, janela, tipo, ROE e status
        ├── Ativo
        │   └── host, URL, APK, endpoint ou outro alvo autorizado
        ├── Checklist
        │   └── cópia versionada de um template de metodologia
        ├── Finding
        │   ├── cópia de template de finding
        │   ├── vínculos com um ou mais ativos
        │   ├── severidade original e contextual
        │   ├── dois eixos de estado
        │   ├── Evidência
        │   └── Retest
        └── Relatório
            └── revisões DOCX, aprovação e PDF derivado
```

O domínio também menciona credenciais de teste, mas sua entidade, ciclo de vida, autorização, uso e descarte ainda não estão definidos. Isso é **decisão pendente**, não autorização para criar um cofre genérico.

A Etapa 0 deve decidir se ownership pertence diretamente a um usuário, a uma organização/equipe ou a outro principal. “Multiusuário” não basta para definir compartilhamento, transferência de propriedade, administradores ou colaboração assíncrona.

---

## 8. Arquitetura

```text
CLI (fops) ─────┐
Burp/integrações ├──► API HTTP em Go ──► PostgreSQL em Docker — dados estruturados
Next.js/React ───┤          │          └► MinIO/S3 — evidências e documentos
Ingestores ──────┘          └──────────► Renderer isolado — DOCX → PDF
```

### Stack definida

**Backend e infraestrutura:**

- Go para backend, API e regras de aplicação;
- `chi` para HTTP;
- `pgx` e `sqlc`, com SQL explícito e tipos gerados; sem ORM;
- `goose` para migrations;
- PostgreSQL executado em Docker no ambiente de desenvolvimento;
- MinIO/S3 para blobs;
- Cobra para a CLI;
- `docxtpl` encapsulado atrás de uma interface/serviço controlado;
- LibreOffice em container isolado para conversão em PDF.

**Frontend:**

- Next.js sobre React, escolhido em vez de uma SPA React isolada para oferecer uma base mais preparada para evolução de rotas, layouts, carregamento de dados, internacionalização e futuras funcionalidades;
- Tailwind CSS como única base de estilização;
- `react-icons` como biblioteca de ícones;
- pnpm como gerenciador de pacotes obrigatório.

`docxtpl` é uma dependência Python. A Etapa 0 deve definir se será sidecar, processo invocado ou serviço interno, além de empacotamento, versionamento, timeout, limites, observabilidade e modelo de falha. Não descrevê-lo como biblioteca Go.

### Regras obrigatórias do frontend

#### Internacionalização

- A interface deve nascer preparada para múltiplos idiomas, sem strings visíveis espalhadas diretamente pelos componentes.
- Textos de interface, mensagens de validação, estados, datas, horários, números e pluralização devem passar pela camada de internacionalização.
- O idioma inicial e os idiomas adicionais serão definidos na Etapa 0.
- A biblioteca de internacionalização deve ser proposta na Etapa 0 com base na versão escolhida do Next.js; não criar uma solução caseira.
- Chaves de tradução devem ser estáveis, organizadas por domínio e verificadas para evitar traduções ausentes.

#### Tema e design tokens

- A aplicação deve suportar tema claro, escuro e preferência do sistema, com persistência da escolha do usuário e sem flash perceptível do tema incorreto no carregamento.
- Cores, tipografia, bordas, raios, sombras, espaçamentos, margens e paddings devem usar tokens do design system expostos pelo Tailwind.
- Componentes não devem conter cores em hexadecimal, RGB, HSL ou nomes de cor soltos, nem valores arbitrários como `p-[13px]`, `mt-[7px]` ou `text-[15px]`.
- Evitar estilos inline para decisões visuais. Exceções realmente dinâmicas devem ser justificadas e encapsuladas.
- Tokens semânticos devem representar intenção, por exemplo: `background`, `surface`, `foreground`, `muted`, `border`, `primary`, `success`, `warning`, `danger`, `info` e níveis de severidade.
- Componentes devem consumir tokens semânticos, não nomes de cores físicas. Alterar a paleta não deve exigir editar cada componente.
- A escala padrão do Tailwind deve ser usada para espaçamento e dimensionamento sempre que possível.
- Famílias, pesos e escalas tipográficas devem ser configurados centralmente e aplicados por classes do Tailwind; fontes não devem ser declaradas individualmente nos componentes.

#### Direção visual

- A paleta deve transmitir segurança, precisão técnica, confiança e legibilidade em dashboards densos.
- A direção inicial recomendada é usar neutros frios nas superfícies, azul ou ciano como ação principal e cores semânticas distintas para sucesso, alerta, erro e severidades.
- A paleta final, com seus tokens e valores, deve ser apresentada para aprovação na Etapa 0; não copiar a identidade visual de outra ferramenta.
- Contraste, foco visível, navegação por teclado e estados que não dependam somente de cor são requisitos.
- Severidade não pode ser comunicada apenas pela cor: deve incluir texto e, quando adequado, ícone ou forma.

#### Ícones

- Usar exclusivamente `react-icons` para os ícones da interface, salvo elemento de marca próprio aprovado.
- Importar apenas os ícones utilizados, evitando importar pacotes inteiros e aumentar o bundle sem necessidade.
- Ícones decorativos devem ser ocultados de tecnologias assistivas; ícones acionáveis precisam de nome acessível e não podem substituir rótulos ambíguos.
- Não usar emojis como substitutos inconsistentes de ícones funcionais.

#### Dependências e pnpm

- pnpm é obrigatório; não misturar npm, Yarn ou outros lockfiles no repositório do frontend.
- O `pnpm-lock.yaml` deve ser versionado e instalações de CI devem usar lockfile congelado.
- Dependências devem usar versões controladas e passar por auditoria; atualizações devem ser revisadas e testadas.
- Scripts de instalação de dependências devem ser negados por padrão ou explicitamente aprovados quando necessários, usando os mecanismos disponíveis na versão adotada do pnpm.
- Dependências não utilizadas, abandonadas ou duplicadas devem ser removidas.
- O uso de pnpm reduz riscos de resolução e instalação quando configurado corretamente, mas não substitui validação de entrada, proteção contra XSS/CSRF, política de conteúdo, atualização de dependências e revisão de supply chain.

### Direção de dependências

O domínio não importa HTTP, PostgreSQL, S3, templates, CLI ou detalhes do renderer.

```text
Next.js/React ──► API HTTP
CLI ────────────► API HTTP
HTTP em Go ─────► application/domain ◄── store
renderer ◄────── application por contrato explícito
```

### API como única porta de entrada

CLI, web UI, extensão de navegador e ingestores são clientes da mesma API HTTP. Somente a implementação de repositórios do servidor acessa o banco.

A única exceção é uma CLI administrativa local para criar o primeiro usuário. Não usar endpoint de bootstrap que dependa apenas de “banco vazio”, pois isso cria corrida de inicialização e pode reativar após wipe/restauração incorreta. O mecanismo de bootstrap, sua autenticação local e seu encerramento definitivo devem ser especificados na Etapa 0.

---

## 9. Segurança

O FrameOPS concentra credenciais de teste, findings não corrigidos e evidências de exploração de clientes reais. Deve ser tratado como sistema de produção e alvo de alto valor, mesmo na fase de uso próprio.

### Requisitos já decididos

- schema multiusuário desde o início;
- ownership como fronteira dura, reforçada por constraints/chaves compostas quando aplicável;
- recurso de outro owner retorna 404, não 403, exceto em fluxos administrativos explicitamente definidos;
- credenciais e segredos cifrados em repouso com chave fora do banco;
- toda mutação estruturada grava auditoria na mesma transação da mutação;
- renderer/conversor isolado, sem acesso de saída à internet e com acesso mínimo aos arquivos necessários;
- sessão revogável e PAT com escopos;
- comparação de segredos em tempo constante quando aplicável;
- princípio do menor privilégio no banco, storage, containers e CI.

### Autenticação

- login deve executar trabalho criptográfico comparável para usuário inexistente, senha errada, conta bloqueada e usuário desativado;
- bloqueio por conta não devolve mensagem distinguível de credencial inválida;
- rate limit por IP pode responder 429; proteção por conta não pode confirmar existência do identificador;
- identificador público de sessão não é segredo de sessão;
- tokens devem possuir entropia criptográfica, armazenamento seguro, expiração e revogação;
- cookies da web devem ter atributos seguros e proteção CSRF compatível com a arquitetura.

“Mesmo tempo” significa reduzir diferenças observáveis e validar estatisticamente os caminhos; não prometer igualdade física impossível.

### Auditoria

Definir eventos, ator, ação, alvo, horário, contexto, resultado e correlação. Auditoria deve ser append-only e não pode registrar segredos, tokens, senhas, credenciais em claro ou conteúdo sensível desnecessário.

A afirmação “toda mutação na mesma transação” vale para dados transacionais no PostgreSQL. Operações em S3/MinIO exigem protocolo de consistência e reconciliação próprio.

### Renderer de documentos

- sem gateway de rede e sem acesso ao banco;
- filesystem temporário e descartável;
- execução sem privilégios, com limites de CPU, memória, processos, tamanho e tempo;
- formatos e recursos externos bloqueados ou controlados;
- entradas tratadas como não confiáveis;
- imagens e relações DOCX validadas;
- logs sem conteúdo do cliente sempre que possível.

### Threat model

Antes da publicação open source — e preferencialmente antes de dados reais — produzir threat model cobrindo pelo menos: API, autenticação, PATs, tenancy/ownership, uploads, arquivos maliciosos, SSRF, parser bombs, object storage, URLs assinadas, supply chain, renderer, backups, logs e estação do operador.

---

## 10. Requisitos não funcionais a definir

Não escolher números arbitrários. Na Etapa 0, propor valores iniciais e seus trade-offs para aprovação:

- tamanho máximo por arquivo, lote e engajamento;
- tipos permitidos e estratégia para tipos desconhecidos;
- timeouts e concorrência de upload, ingestão e renderização;
- paginação e limites da API;
- metas de latência para operações interativas e captura;
- política de retenção;
- backup, restauração e teste periódico de restore;
- RPO e RTO compatíveis com uso próprio inicial;
- métricas, logs estruturados, traces e alertas mínimos;
- disponibilidade e modo degradado;
- versões de PostgreSQL, MinIO, Go, Next.js, React, Tailwind CSS, pnpm, Python/docxtpl e LibreOffice suportadas;
- plataformas suportadas pela CLI;
- acessibilidade e compatibilidade de navegador da web UI.

Segredos ou conteúdo de evidência não devem aparecer em logs, métricas, traces, erros ou fixtures.

---

## 11. Plano de implementação

Cada etapa termina em algo executável, testado e demonstrável. O agente não deve interpretar a tabela como permissão para resolver decisões pendentes sozinho.

| # | Etapa | Entrega verificável |
|---|-------|---------------------|
| 0 | Alinhamento | revisão crítica, contradições, threat sketch, decisões pendentes e critérios de aceite; sem código de produto |
| 1 | Estrutura | árvore de diretórios, toolchains Go/pnpm fixadas, Docker Compose com PostgreSQL, lint, testes mínimos e documentação de desenvolvimento |
| 2 | Infra e schema | PostgreSQL, MinIO, migrations, constraints, triggers e testes reais de schema |
| 3 | Domínio | CVSS aprovado, máquinas de estado, invariantes e contratos de repositório |
| 4 | API e autenticação | sessão revogável, PAT, autorização/ownership, auditoria e CRUD base |
| 5 | Findings | templates imutavelmente copiados, findings e vínculos com ativos |
| 6 | Evidências | streaming, hash server-side, detecção de tipo, append-only, reconciliação e acesso assinado |
| 7 | Metodologias | sete templates versionados, com conteúdo real e atribuições; meta inicial de 250–350 itens |
| 8 | Retest | rodadas, histórico, comparação e SLA somente se sua semântica estiver aprovada |
| 9 | CLI `fops` | contexto ativo, captura sem atrito, screenshot e ingestão de Nmap/ffuf/Nuclei |
| 10 | Relatórios | DOCX, revisões reimportadas, aprovação e PDF derivado com proveniência |
| 11 | Web UI | frontend Next.js/React com i18n, temas, tokens Tailwind, acessibilidade, portfólio, checklist, findings, evidências, retests e relatórios |
| 12 | Open source | threat model final, CI, documentação, licença, atribuições, processo de disclosure e auditoria do histórico Git |

### Razões da ordem

- **Domínio antes de HTTP:** CVSS e máquinas de estado são regras puras e determinísticas; vetores do FIRST e matriz de transições oferecem testes objetivos.
- **API antes da CLI:** impede que CLI e web desenvolvam regras ou acesso a dados divergentes.
- **Evidências antes da CLI:** a CLI só resolve captura se o mecanismo seguro de anexação já existir.
- **Relatório após seus dados de origem:** findings, evidências, checklist e retest alimentam o entregável.
- **Dashboard por último:** é dado derivado; construir a vitrine antes do fluxo operacional mascara a dor central.

### Gates mínimos de toda etapa com código

- formatação e lint;
- testes unitários das regras alteradas;
- testes de integração com dependências reais quando a etapa toca persistência, storage ou conversão;
- testes negativos e de autorização para superfícies de segurança;
- migrations aplicadas do zero e sobre a versão anterior suportada;
- build reproduzível;
- frontend sem valores visuais raw ou arbitrários fora das exceções justificadas e sem strings de interface fora da camada de i18n;
- instalação do frontend reproduzível com pnpm e lockfile congelado;
- documentação do comportamento entregue;
- nenhuma credencial, evidência real ou segredo em repositório, fixture ou log;
- revisão dos critérios de aceite da etapa.

---

## 12. Estratégia de testes

Teste deve cobrir a superfície real, não apenas exemplos lembrados pelo desenvolvedor.

- toda rota registrada exige autenticação, salvo lista fechada e revisada de exceções;
- toda rota mutável exige autorização e auditoria;
- toda rota com identificador testa isolamento entre owners;
- toda constraint relevante do banco possui tradução estável para resposta de domínio/HTTP;
- toda transição de estado é enumerada e testada como permitida ou negada;
- vetores oficiais de CVSS validam parsing, cálculo e arredondamento;
- upload testa conteúdo truncado, tamanho excedido, tipo falso, interrupção, duplicidade, hash e reconciliação;
- ingestores usam corpus versionado com entradas válidas, versões diferentes, arquivos incompletos e payloads hostis;
- relatório testa proveniência: o PDF deve apontar para o hash exato do DOCX aprovado;
- restore de backup deve ser exercitado, não apenas documentado.

A fonte de enumeração deve ser o sistema real — roteador registrado, schema aplicado, catálogo de estados ou registro de ingestores. Uma lista manual isolada não prova que um novo recurso entrou na cobertura.

---

## 13. Decisões e armadilhas já conhecidas

- **Evidência nunca cascateia junto com finding.** A exclusão lógica ou mudança de estado de um finding não apaga sua cadeia de custódia.
- **Migration aplicada é imutável.** Toda correção avança o histórico.
- **Falhas de login não funcionam como oráculo de conta.** Mensagem e custo criptográfico devem evitar enumeração trivial.
- **Bloqueio por conta não recebe resposta distinta.** Rate limit por IP pode ser observável; existência da conta, não.
- **UUID de sessão não substitui segredo aleatório.** Identificador e credencial são conceitos diferentes.
- **Detecção de tipo consome bytes do stream.** Esses bytes devem ser recolocados no fluxo persistido; hash igual ao objeto não detecta truncamento se ambos usam o mesmo stream já consumido.
- **Object storage não participa da transação SQL.** Órfãos e estados intermediários exigem reconciliação.
- **Template é copiado, não referenciado de forma viva.** Histórico entregue não muda com edição da biblioteca.
- **PDF vem do DOCX aprovado.** Nunca há renderização paralela independente.
- **Severidade importada é preservada.** Classificação contextual não apaga a fonte.

---

## 14. Etapa 0 — revisão obrigatória antes de implementar

A primeira resposta do agente deve ser uma revisão crítica, não código. Ela deve conter:

### 14.1 Resumo do entendimento

Resuma objetivo, usuário inicial, fluxo principal, fronteiras do produto e invariantes. Não reescreva este documento inteiro.

### 14.2 Contradições e tensões iniciais a resolver

Analise pelo menos estas questões:

1. **“Evidência indeletável” versus retenção, privacidade e obrigação legal:** definir imutabilidade lógica, descarte autorizado, legal hold e permanência de metadados.
2. **Auditoria na mesma transação versus blobs fora do PostgreSQL:** definir estados, compensação e reconciliação sem alegar atomicidade distribuída.
3. **Multiusuário versus ownership:** decidir se o principal é usuário, equipe ou organização e como ocorre compartilhamento/administração.
4. **Go versus `docxtpl` em Python:** definir fronteira, deploy, falhas e segurança do componente.
5. **Reimportação manual versus proveniência:** definir revisões imutáveis, aprovação, invalidação e relação DOCX→PDF.
6. **Finding removível versus evidência permanente:** definir exclusão lógica, tombstone e autorização; não usar cascade destrutivo.
7. **Credenciais cifradas versus domínio ainda sem entidade de credencial:** decidir se entram no MVP e qual ciclo de vida possuem.
8. **CLI de cinco segundos versus rede indisponível:** decidir se offline é requisito; se for, proteger e reconciliar a fila local.
9. **Sete metodologias e 250–350 itens versus licenças/manutenção:** definir fontes, versões, atribuição, revisão técnica e atualização.
10. **CVSS 3.1 próprio versus evolução do padrão:** justificar implementação própria, testar contra FIRST e decidir estratégia para CVSS 4.0.
11. **404 entre owners versus futuros papéis administrativos:** definir quando acesso administrativo existe e como é auditado sem vazar existência ao usuário comum.
12. **Open source posterior versus dados reais desde cedo:** garantir configuração segura por padrão, política de disclosure e limpeza do histórico antes da publicação.

Procure outras contradições além da lista. Classifique cada item como bloqueador, importante ou adiável.

### 14.3 Decisões pendentes

Para cada decisão:

- pergunta objetiva;
- por que precisa ser decidida agora ou por que pode esperar;
- duas ou três opções viáveis;
- trade-offs de segurança, produto e operação;
- recomendação do agente;
- impacto nas etapas;
- decisão do responsável, inicialmente marcada como pendente.

### 14.4 Threat sketch inicial

Identifique ativos, atores, fronteiras de confiança, entradas não confiáveis, ameaças principais e controles propostos. Nesta etapa basta um sketch acionável; o threat model completo evolui com a arquitetura.

### 14.5 Critérios de aceite por etapa

Transforme as entregas da tabela em critérios observáveis, sem inventar requisitos de produto. Números, limites e políticas ainda não aprovados devem permanecer como pendentes.

### 14.6 Plano revisado

Aponte dependências que exijam reordenar ou dividir etapas. Não mude a ordem apenas por preferência pessoal; explique o risco concreto evitado.

### 14.7 Gate de aprovação

Finalize a Etapa 0 com uma lista curta do que precisa de resposta. **Não gere código, scaffold ou migration até receber aprovação explícita das decisões bloqueadoras.**

---

## 15. Definição final de sucesso

O FrameOPS terá cumprido sua proposta quando um pentest real puder ser conduzido do início ao relatório entregue com:

- escopo e metodologia rastreáveis;
- captura de findings e evidências no fluxo de trabalho existente;
- evidências íntegras e recuperáveis;
- estado de validação e remediação sem ambiguidades;
- retests historicamente verificáveis;
- relatório editável, aprovado e convertido com proveniência;
- isolamento entre owners demonstrado por testes;
- restauração de dados comprovada;
- nenhuma necessidade operacional de manter notas paralelas no Obsidian.

O produto não deve ser considerado bem-sucedido apenas porque possui CRUD, dashboard ou relatório visualmente correto. O teste decisivo é reduzir atrito sem sacrificar integridade, segurança ou rastreabilidade.
