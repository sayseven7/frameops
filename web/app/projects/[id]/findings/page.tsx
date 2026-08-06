import { Workspace } from "../../../page";

type ProjectPageProps = { params: Promise<{ id: string }> };

export default async function ProjectFindingsPage({ params }: ProjectPageProps) {
  const { id } = await params;
  return <Workspace initialSection="findings" initialProjectID={id} />;
}
