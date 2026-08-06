import { Workspace } from "../../../page";

type ProjectPageProps = { params: Promise<{ id: string }> };

export default async function ProjectOverviewPage({ params }: ProjectPageProps) {
  const { id } = await params;
  return <Workspace initialSection="overview" initialProjectID={id} />;
}
