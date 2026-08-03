const sections = [
  {
    id: "portfolio",
    label: "Portfólio",
    eyebrow: "Visão de trabalho",
    title: "Engajamentos em acompanhamento",
    description:
      "A demonstração organiza o trabalho por cliente e engajamento, sem afirmar atividade ou dados operacionais reais.",
    items: ["Cliente demonstrativo: Atlas", "Engajamento: Revisão web", "Escopo ilustrativo: aplicações públicas"],
  },
  {
    id: "engagement",
    label: "Engajamento",
    eyebrow: "Contexto",
    title: "Revisão web — exemplo estático",
    description:
      "Um local único para consolidar escopo, ativos autorizados e regras de engajamento quando os contratos de domínio existirem.",
    items: ["Janela: informação demonstrativa", "Ativo: portal.exemplo.test", "Regra: somente o escopo autorizado"],
  },
  {
    id: "checklist",
    label: "Checklist",
    eyebrow: "Metodologia",
    title: "Cobertura orientada por procedimento",
    description:
      "Itens de metodologia ajudam a registrar o que foi verificado, a evidência esperada e a justificativa quando necessário.",
    items: ["Autenticação — procedimento de exemplo", "Entrada de dados — procedimento de exemplo", "Sessão — procedimento de exemplo"],
  },
  {
    id: "findings",
    label: "Findings",
    eyebrow: "Registro estruturado",
    title: "Finding demonstrativo",
    description:
      "A classificação abaixo é apenas uma amostra visual; não representa uma vulnerabilidade, avaliação ou estado real.",
    items: ["Severidade: Alta — exemplo", "Validação: precisa de revisão — exemplo", "Remediação: não exibida neste preview"],
  },
  {
    id: "evidence",
    label: "Evidências",
    eyebrow: "Cadeia de custódia",
    title: "Referência de evidência sintética",
    description:
      "No produto, cada evidência será vinculada a origem, hash e histórico. Este preview não contém arquivos, uploads ou dados reais.",
    items: ["Tipo: captura ilustrativa", "Origem: não conectada", "Integridade: indisponível neste preview"],
  },
  {
    id: "retest",
    label: "Retest",
    eyebrow: "Histórico",
    title: "Rodada de retest — exemplo",
    description:
      "Retests futuros preservarão rodadas e resultados observados. Nenhum resultado é calculado ou armazenado nesta página.",
    items: ["Rodada: exemplo não persistente", "Procedimento: demonstrativo", "Resultado: não avaliado"],
  },
  {
    id: "reports",
    label: "Relatórios",
    eyebrow: "Entrega",
    title: "Rastreabilidade de relatório",
    description:
      "O fluxo planejado mantém revisões DOCX e PDF derivado. Geração, aprovação e arquivos permanecem fora deste preview.",
    items: ["DOCX: não gerado", "Revisão: não disponível", "PDF: não disponível"],
  },
];

export default function HomePage() {
  return (
    <div className="app-shell">
      <header className="site-header">
        <a className="brand" href="#portfolio">
          FrameOPS <span>preview</span>
        </a>
        <p className="preview-notice" role="status">
          Preview técnico — dados sintéticos; sem API, autenticação, persistência ou dados reais.
        </p>
      </header>

      <div className="workspace">
        <aside className="sidebar" aria-label="Navegação do preview">
          <nav aria-label="Áreas do preview">
            <p className="nav-label">Navegação</p>
            <ul>
              {sections.map(({ id, label }) => (
                <li key={id}>
                  <a href={`#${id}`}>{label}</a>
                </li>
              ))}
            </ul>
          </nav>
        </aside>

        <main className="content" id="conteudo">
          <section className="intro" aria-labelledby="preview-title">
            <p className="eyebrow">Ambiente de demonstração</p>
            <h1 id="preview-title">Planejamento e consolidação, sem fingir operação.</h1>
            <p>
              Esta página mostra a estrutura planejada do FrameOPS. Ela é local, estática e intencionalmente desconectada do produto funcional.
            </p>
          </section>

          {sections.map(({ id, eyebrow, title, description, items }) => (
            <section className="preview-section" id={id} key={id} aria-labelledby={`${id}-title`}>
              <p className="eyebrow">{eyebrow}</p>
              <h2 id={`${id}-title`}>{title}</h2>
              <p>{description}</p>
              <ul className="detail-list">
                {items.map((item) => (
                  <li key={item}>{item}</li>
                ))}
              </ul>
            </section>
          ))}
        </main>
      </div>
    </div>
  );
}
