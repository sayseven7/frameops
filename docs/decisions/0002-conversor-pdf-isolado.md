# ADR 0002: fronteira do conversor DOCX → PDF

**Status:** padrão mínimo em vigor; empacotamento definitivo é decisão pendente
da Etapa 0 (item 4: "Go versus `docxtpl` em Python").

## Contexto

O PRODUCT.md exige que a geração de documentos e a conversão para PDF rodem em
um worker isolado, sem acesso direto a banco de dados, object storage ou
internet, e que o PDF seja derivado exclusivamente da revisão DOCX aprovada.

O brief mestre indica "LibreOffice em container isolado" e deixa em aberto
empacotamento, versionamento, timeout, limites, observabilidade e modelo de
falha. Essa decisão não pode ser inventada durante a implementação, mas a fatia
de entrega do PDF derivado precisa de um contrato executável agora.

## Decisão (padrão mínimo)

1. O worker é um executável separado, `cmd/frameops-render`, invocado como
   subprocesso pela API. Ele recebe dois caminhos de arquivo (`--source`,
   `--destination`) e responde com uma linha JSON contendo `converter`,
   `sha256` e `byteSize`.
2. O worker não importa nenhum pacote de banco ou de object storage e recusa
   iniciar se enxergar qualquer variável de ambiente fora do conjunto fechado
   `PATH`, `HOME`, `TMPDIR`, `LANG`, `LC_ALL`, `TZ`. Uma credencial visível ao
   worker é falha de inicialização, não um detalhe ignorado.
3. O conversor roda dentro de um namespace de rede vazio
   (`unshare --net --map-root-user`). Se o namespace não puder ser criado, não
   há conversão: a falha nunca produz artefato que possa ser confundido com
   entregável.
4. O conversor local aprovado por padrão é o LibreOffice headless disponível no
   ambiente (`soffice`), com perfil de usuário próprio por conversão. O caminho
   do worker é configurado por `FRAMEOPS_PDF_WORKER`; a API recusa iniciar sem
   ele, porque não existe modo degradado de entrega.
5. Limites em vigor: entrada de até 32 MiB (mesmo limite da importação de
   revisões), saída de até 64 MiB, dois minutos de timeout dentro do worker e
   três minutos no lado da API.
6. Proveniência: cada PDF entregue registra o digest da revisão DOCX aprovada
   de origem, a identificação do conversor, o próprio digest e o tamanho, com
   eventos de auditoria `report.pdf.derived` e `report.pdf.stored`.

## Consequências

O empacotamento em container dedicado, a fixação de versão do conversor por
imagem com digest e a observabilidade do worker continuam pendentes. Quando
forem aprovados, apenas a forma de invocar o worker muda: o contrato de entrada,
de saída e de proveniência já está fixado aqui.

Ambientes sem `unshare` ou sem LibreOffice não convertem; a rota responde
`conversion_failed` e nenhuma revisão aprovada é marcada como entregue.

Nenhuma credencial nova foi criada por esta decisão. `FRAMEOPS_PDF_WORKER` é
configuração de caminho, não segredo.
