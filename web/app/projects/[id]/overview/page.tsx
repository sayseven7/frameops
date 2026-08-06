import ProtectedWorkspace from "../../../protected-workspace";

type ProjectPageProps = { params: Promise<{ id: string }> };

export default async function ProjectOverviewPage({ params }: ProjectPageProps) {
  const { id } = await params;
  return <ProtectedWorkspace initialSection="overview" initialProjectID={id} />;
}
