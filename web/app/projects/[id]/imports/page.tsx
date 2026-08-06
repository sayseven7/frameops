import ProtectedWorkspace from "../../../protected-workspace";

type ProjectPageProps = { params: Promise<{ id: string }> };

export default async function ProjectImportsPage({ params }: ProjectPageProps) {
  const { id } = await params;
  return <ProtectedWorkspace initialSection="imports" initialProjectID={id} />;
}