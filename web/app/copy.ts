export const copy = {
  "pt-BR": {
    language: "Idioma",
    english: "English",
    portuguese: "Português (Brasil)",
    title: "FrameOPS",
    subtitle: "Operação de portfólio",
    signIn: "Entrar",
    email: "E-mail",
    password: "Senha",
    signInAction: "Iniciar sessão",
    signingIn: "Iniciando sessão…",
    portfolio: "Portfólio",
    clients: "Clientes",
    clientName: "Nome do cliente",
    createClient: "Criar cliente",
    creatingClient: "Criando cliente…",
    selectClient: "Selecione um cliente",
    engagements: "Engagements",
    engagementName: "Nome do engagement",
    createEngagement: "Criar engagement",
    creatingEngagement: "Criando engagement…",
    noClients: "Nenhum cliente encontrado.",
    noEngagements: "Nenhum engagement encontrado.",
    sessionReady: "Sessão iniciada.",
    loading: "Carregando…",
    errorGeneric: "Não foi possível concluir a operação. Tente novamente.",
    errorSession: "Sua sessão expirou. Entre novamente.",
    errorInvalidRequest: "Confira os dados e tente novamente.",
    errorForbidden: "Você não tem permissão para esta operação.",
    errorNotFound: "O item solicitado não foi encontrado.",
    noClientSelected: "Selecione um cliente antes de criar um engagement.",
  },
  en: {
    language: "Language",
    english: "English",
    portuguese: "Português (Brasil)",
    title: "FrameOPS",
    subtitle: "Portfolio operations",
    signIn: "Sign in",
    email: "Email",
    password: "Password",
    signInAction: "Start session",
    signingIn: "Starting session…",
    portfolio: "Portfolio",
    clients: "Clients",
    clientName: "Client name",
    createClient: "Create client",
    creatingClient: "Creating client…",
    selectClient: "Select a client",
    engagements: "Engagements",
    engagementName: "Engagement name",
    createEngagement: "Create engagement",
    creatingEngagement: "Creating engagement…",
    noClients: "No clients found.",
    noEngagements: "No engagements found.",
    sessionReady: "Session started.",
    loading: "Loading…",
    errorGeneric: "We could not complete the operation. Try again.",
    errorSession: "Your session expired. Sign in again.",
    errorInvalidRequest: "Check the information and try again.",
    errorForbidden: "You do not have permission for this operation.",
    errorNotFound: "The requested item was not found.",
    noClientSelected: "Select a client before creating an engagement.",
  },
} as const;

export type Locale = keyof typeof copy;

export function apiErrorMessage(code: string, locale: Locale) {
  const text = copy[locale];
  switch (code) {
    case "unauthorized":
    case "invalid_credentials":
      return text.errorSession;
    case "invalid_request":
      return text.errorInvalidRequest;
    case "forbidden":
      return text.errorForbidden;
    case "not_found":
      return text.errorNotFound;
    default:
      return text.errorGeneric;
  }
}
