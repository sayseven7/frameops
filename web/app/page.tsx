const navigation = [
  ["portfolio", "Portfólio"],
  ["engagement", "Engajamento"],
  ["checklist", "Checklist"],
  ["findings", "Findings"],
  ["evidence", "Evidências"],
  ["retest", "Retest"],
  ["reports", "Relatórios"],
] as const;

const activity = [
  ["11:42", "Evidência vinculada", "Captura ilustrativa · origem não conectada"],
  ["10:18", "Finding em revisão", "Alta · demonstração sem avaliação real"],
  ["09:05", "Checklist atualizado", "Sessão · item demonstrativo"],
] as const;

export default function HomePage() {
  return (
    <div className="app-shell">
      <header className="topbar">
        <a className="brand" href="#portfolio" aria-label="FrameOPS, ir para o portfólio">
          <span className="brand-mark" aria-hidden="true">F7</span>
          <span>FRAMEOPS</span>
        </a>
        <p className="demo-notice" role="status">
          <span aria-hidden="true">●</span> DEMO ESTÁTICA · dados sintéticos · sem API, autenticação, persistência ou dados reais
        </p>
        <a className="command-link" href="#reports">Ver entrega <span aria-hidden="true">↗</span></a>
      </header>

      <div className="workspace">
        <aside className="sidebar" aria-label="Navegação operacional">
          <div className="operator">
            <span className="operator-dot" aria-hidden="true" />
            <div><strong>ATLAS / DEMO</strong><small>Revisão web · local</small></div>
          </div>
          <nav>
            <p className="nav-title">Operação</p>
            <ul>
              {navigation.map(([id, label], index) => (
                <li key={id}>
                  <a className={index === 0 ? "active" : ""} href={`#${id}`}>
                    <span className="nav-index">0{index + 1}</span>{label}
                  </a>
                </li>
              ))}
            </ul>
          </nav>
          <div className="sidebar-footer">
            <span>AMBIENTE</span>
            <strong>LOCAL / ILUSTRATIVO</strong>
          </div>
        </aside>

        <main className="content">
          <section className="command-deck" id="portfolio" aria-labelledby="deck-title">
            <div className="deck-heading">
              <div>
                <p className="route">PORTFÓLIO / ATLAS / REVISÃO WEB</p>
                <h1 id="deck-title">Superfície de ataque e evidência.</h1>
                <p>Um workspace demonstrativo para registrar o trabalho que, no produto, passará de descoberta a relatório rastreável.</p>
              </div>
              <div className="engagement-state" aria-label="Estado do engajamento: demonstração em revisão">
                <span>EM REVISÃO</span>
                <strong>DEMO</strong>
              </div>
            </div>

            <div className="attack-surface" aria-label="Mapa demonstrativo da superfície de ataque">
              <div className="surface-header"><span>SUPERFÍCIE AUTORIZADA</span><span>3 ALVOS ILUSTRATIVOS</span></div>
              <div className="surface-map">
                <div className="surface-node primary"><span>portal.exemplo.test</span><small>WEB / EXEMPLO</small></div>
                <div className="surface-node"><span>api.exemplo.test</span><small>API / EXEMPLO</small></div>
                <div className="surface-node"><span>admin.exemplo.test</span><small>PAINEL / EXEMPLO</small></div>
                <i className="trace trace-one" aria-hidden="true" /><i className="trace trace-two" aria-hidden="true" />
              </div>
              <div className="surface-legend"><span><b className="legend-safe" /> Escopo ilustrativo</span><span><b className="legend-review" /> Revisão necessária</span><span>SEM CONEXÃO</span></div>
            </div>
          </section>

          <section className="operations-grid" aria-label="Resumo operacional demonstrativo">
            <article className="operation-panel" id="engagement">
              <header><span>ENGAJAMENTO</span><a href="#checklist">Abrir escopo <span aria-hidden="true">→</span></a></header>
              <h2>Revisão web</h2>
              <dl className="compact-list"><div><dt>Cliente</dt><dd>Atlas <em>DEMO</em></dd></div><div><dt>Janela</dt><dd>Informação ilustrativa</dd></div><div><dt>Regra</dt><dd>Somente escopo autorizado</dd></div></dl>
            </article>

            <article className="operation-panel checklist-panel" id="checklist">
              <header><span>CHECKLIST</span><a href="#findings">Metodologia <span aria-hidden="true">→</span></a></header>
              <h2>Cobertura de procedimento</h2>
              <ul className="checklist"><li><span aria-hidden="true">✓</span> Autenticação <small>exemplo</small></li><li><span aria-hidden="true">✓</span> Entrada de dados <small>exemplo</small></li><li><span aria-hidden="true">○</span> Sessão <small>aguarda revisão</small></li></ul>
            </article>

            <article className="findings-panel" id="findings">
              <div><span className="panel-label">FINDING / 01</span><span className="severity"><b aria-hidden="true">!</b> ALTA · EXEMPLO</span></div>
              <h2>Registro demonstrativo, não avaliação.</h2>
              <p>O estado pede revisão humana e não descreve uma vulnerabilidade real.</p>
              <a href="#evidence">Examinar evidência vinculada <span aria-hidden="true">→</span></a>
            </article>

            <article className="evidence-panel" id="evidence">
              <header><span>EVIDÊNCIAS</span><span>0 ARQUIVOS REAIS</span></header>
              <div className="evidence-sheet">
                <div className="evidence-code" aria-hidden="true"><span /><span /><span /><span /><span /></div>
                <div><strong>captura-ilustrativa.txt</strong><p>Origem não conectada · hash indisponível no preview</p></div>
                <span className="evidence-tag">SINTÉTICA</span>
              </div>
            </article>

            <article className="activity-panel" id="retest">
              <header><span>RETEST / LINHA DE TEMPO</span><span>NÃO PERSISTENTE</span></header>
              <ol>{activity.map(([time, title, detail]) => <li key={time}><time>{time}</time><div><strong>{title}</strong><small>{detail}</small></div></li>)}</ol>
            </article>

            <article className="reports-panel" id="reports">
              <span className="panel-label">RELATÓRIOS</span><h2>Entrega só existe após aprovação.</h2>
              <p>DOCX e PDF continuam indisponíveis neste preview estático.</p>
              <div><span>DOCX</span><strong>NÃO GERADO</strong><span>PDF</span><strong>NÃO DISPONÍVEL</strong></div>
            </article>
          </section>
        </main>
      </div>
    </div>
  );
}