# ADR 0001: matriz de suporte das toolchains

**Status:** aprovado

## Contexto

A Etapa 1 precisa permitir que o ambiente de desenvolvimento e os gates locais
falhem cedo quando uma ferramenta não atende à versão mínima aprovada. A matriz
é registrada aqui para que `.tool-versions`, `go.mod`, `package.json` e os
checks de shell sejam verificáveis contra a mesma decisão.

## Decisão

| Ferramenta | Versão aprovada | Contrato aplicado |
| --- | --- | --- |
| Go | >= 1.26.5 | `go.mod`, `.tool-versions` e `scripts/check-toolchains.sh` |
| Node.js | >= 22.12.0 | `.tool-versions` e `scripts/check-toolchains.sh` |
| pnpm | major 10 | `.tool-versions`, `package.json` e `scripts/check-toolchains.sh` |
| Python | >= 3.13.0 | `.tool-versions` e `scripts/check-toolchains.sh` |
| Docker Compose | >= 2.20.0 | `scripts/check-toolchains.sh` |
| golangci-lint | v2.9.0, compilado com Go 1.26.5 | disponibilidade exigida por `scripts/check-toolchains.sh` |

O frontend usa exclusivamente pnpm. O módulo Go é
`github.com/sayseven7/frameops`.

## Consequências

`scripts/check-toolchains.sh` valida formatos de versão estáveis e recusa
versões abaixo dos pisos. `scripts/check_toolchains_test.sh` cobre os pisos,
rejeições e versões de pré-lançamento para Go e Python, incluindo Docker
Compose abaixo de 2.20.0.

Nenhuma imagem Docker, credencial, dado real ou artefato de evidência é
necessário para esse contrato.
